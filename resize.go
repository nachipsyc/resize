package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	jpegstructure "github.com/dsoprea/go-jpeg-image-structure/v2"
	"github.com/nfnt/resize"
)

var sourceDir, targetDir string
var lowerLimit int64
var magnification float64

func main() {
	flag.Parse()
	sourceDir = flag.Arg(0)
	targetDir = flag.Arg(1)
	lowerLimit, _ = strconv.ParseInt(flag.Arg(2), 10, 64)
	magnification, _ = strconv.ParseFloat(flag.Arg(3), 64)

	files, err := os.ReadDir(sourceDir)
	if err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 4) // 並列処理の最大数を制限

	for _, file := range files {
		if isJpeg(file.Name()) {
			wg.Add(1)
			go func(file os.DirEntry) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				processImage(file)
			}(file)
		}
	}

	wg.Wait()
	fmt.Println("すべての画像処理が完了しました。")
}

func processImage(file os.DirEntry) {
	sourcePath := filepath.Join(sourceDir, file.Name())
	targetPath := filepath.Join(targetDir, file.Name())

	// EXIFデータを取得
	exifData, err := getExifData(sourcePath)
	if err != nil {
		log.Printf("EXIF情報の取得に失敗: %v\n", err)
	}

	// 画像を開く
	imgFile, err := os.Open(sourcePath)
	if err != nil {
		log.Printf("画像の読み込みエラー: %v\n", err)
		return
	}
	defer imgFile.Close()

	// 画像をデコード
	img, _, err := image.Decode(imgFile)
	if err != nil {
		log.Printf("画像のデコードエラー: %v\n", err)
		return
	}

	// ファイルサイズチェック
	fileInfo, _ := imgFile.Stat()
	fileSize := fileInfo.Size()
	if fileSize < lowerLimit {
		// lowerLimit 未満ならそのままコピー
		copyFile(sourcePath, targetPath)
		return
	}

	// リサイズ処理
	resizedImg := resizeImage(img, fileSize)

	// EXIFデータを保持して保存
	saveImageWithExif(resizedImg, targetPath, exifData)
}

// **EXIFデータを取得**
func getExifData(filePath string) ([]byte, error) {
	parser := jpegstructure.NewJpegMediaParser()
	intfc, err := parser.ParseFile(filePath)
	if err != nil {
		return nil, err
	}

	sl := intfc.(*jpegstructure.SegmentList)
	segments := sl.Segments()

	// EXIFセグメントを探して、そのデータをそのまま返す
	for _, segment := range segments {
		if segment.MarkerId == jpegstructure.MARKER_APP1 {
			return segment.Data, nil
		}
	}

	return nil, fmt.Errorf("EXIF data not found")
}

// **EXIFデータを保持してJPEGを保存**
func saveImageWithExif(img image.Image, targetPath string, exifData []byte) {
	// メモリ上にJPEGをエンコード
	buf := new(bytes.Buffer)
	options := &jpeg.Options{Quality: 100}
	err := jpeg.Encode(buf, img, options)
	if err != nil {
		log.Printf("JPEGエンコードエラー: %v\n", err)
		return
	}

	// **新しいJPEGデータにEXIFを埋め込む**
	parser := jpegstructure.NewJpegMediaParser()
	intfc, err := parser.ParseBytes(buf.Bytes())
	if err != nil {
		log.Printf("JPEGパースエラー: %v\n", err)
		return
	}

	sl := intfc.(*jpegstructure.SegmentList)

	// APP1セグメントとしてEXIFを追加
	segment := &jpegstructure.Segment{
		MarkerId: jpegstructure.MARKER_APP1,
		Data:     append([]byte("Exif\x00\x00"), exifData...),
	}
	sl.Add(segment) // セグメントを追加

	outFile, err := os.Create(targetPath)
	if err != nil {
		log.Printf("保存エラー: %v\n", err)
		return
	}
	defer outFile.Close()

	err = sl.Write(outFile)
	if err != nil {
		log.Printf("JPEGの書き込みエラー: %v\n", err)
	}
}

// **画像のリサイズ**
func resizeImage(img image.Image, fileSize int64) image.Image {
	if magnification != 0 {
		newWidth := uint(float64(img.Bounds().Dx()) * magnification)
		return resize.Resize(newWidth, 0, img, resize.Lanczos3)
	}

	// 90%ずつ縮小しながら lowerLimit 以下になるまでループ
	resizedImg := img
	scale := 0.9
	for fileSize >= lowerLimit {
		newWidth := uint(float64(resizedImg.Bounds().Dx()) * scale)
		resizedImg = resize.Resize(newWidth, 0, resizedImg, resize.Lanczos3)
		fileSize = estimateJPEGSize(resizedImg)
	}
	return resizedImg
}

// **画像のコピー**
func copyFile(src, dst string) {
	sourceFile, err := os.Open(src)
	if err != nil {
		log.Printf("コピー元のファイルを開けません: %v\n", err)
		return
	}
	defer sourceFile.Close()

	targetFile, err := os.Create(dst)
	if err != nil {
		log.Printf("コピー先のファイルを作成できません: %v\n", err)
		return
	}
	defer targetFile.Close()

	_, err = io.Copy(targetFile, sourceFile)
	if err != nil {
		log.Printf("ファイルのコピーに失敗: %v\n", err)
	}
}

// **JPEGファイルかどうかを判定**
func isJpeg(filename string) bool {
	ext := filepath.Ext(filename)
	return ext == ".jpg" || ext == ".jpeg" || ext == ".JPG" || ext == ".JPEG"
}

// **圧縮後のJPEGサイズを推定**
func estimateJPEGSize(img image.Image) int64 {
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()
	return int64(width * height * 3 / 10) // 10:1の圧縮を仮定
}
