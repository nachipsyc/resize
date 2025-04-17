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
	"sync"

	jpegstructure "github.com/dsoprea/go-jpeg-image-structure/v2"
	"github.com/nfnt/resize"
)

var (
	sourceDir  string
	targetDir  string
	lowerLimit int64
)

func main() {
	flag.StringVar(&sourceDir, "source", "", "ソースディレクトリのパス")
	flag.StringVar(&targetDir, "target", "", "ターゲットディレクトリのパス")
	flag.Int64Var(&lowerLimit, "lower", 0, "ファイルサイズの下限")

	// 入力をパース
	flag.Parse()

	// 引数の検証
	if sourceDir == "" || targetDir == "" {
		fmt.Println("Command Error: ソースディレクトリとターゲットディレクトリの両方を指定してください。")
		fmt.Println("使い方:")
		fmt.Println("  -source <path>  : ソース画像ディレクトリのパス")
		fmt.Println("  -target <path>  : ターゲット画像ディレクトリのパス")
		fmt.Println("  -lower <size>   : ファイルサイズの下限")
		return
	}

	// ソースディレクトリを読み込む
	files, err := os.ReadDir(sourceDir)
	if err != nil {
		log.Fatal(err)
	}

	// JPEG画像の数をカウント
	jpegCount := 0
	for _, file := range files {
		if isJpeg(file.Name()) {
			jpegCount++
		}
	}

	// 進捗カウンター用の変数
	processedCount := 0
	var countMutex sync.Mutex

	// ログのフォーマットを設定
	log.SetFlags(log.LstdFlags) // 日時を表示

	// 並列処理のためのWaitGroupとセマフォを初期化
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)

	// ファイルを処理
	for _, file := range files {
		if isJpeg(file.Name()) {
			wg.Add(1)
			go func(file os.DirEntry) {
				defer wg.Done()
				sem <- struct{}{}

				// 処理開始時にカウンターを更新
				countMutex.Lock()
				processedCount++
				currentCount := processedCount // ローカルにコピー
				countMutex.Unlock()

				// ログメッセージに進捗を含める形に修正
				processImageWithProgress(file, currentCount, jpegCount)

				<-sem
			}(file)
		}
	}

	wg.Wait()
	fmt.Println("\nすべての画像処理が完了しました。")
}

// **画像の処理**
func processImage(file os.DirEntry) {
	// 入力元と出力先のパスを設定
	sourcePath := filepath.Join(sourceDir, file.Name())
	targetPath := filepath.Join(targetDir, file.Name())

	// EXIFデータを取得（なくてもエラーを出さずに処理を続ける）
	exifData, err := getExifData(sourcePath)
	if err != nil {
		log.Printf("EXIF情報なし: %s（通常処理を継続）\n", file.Name())
	} else {
		log.Printf("EXIF情報取得成功: %s\n", file.Name())
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
	resizedImg, quality := resizeImage(img)

	// EXIFデータがある場合は保持、ない場合はそのまま保存
	saveImageWithExif(resizedImg, targetPath, exifData, quality)
}

// **EXIFデータを取得（エラーでも処理を続ける）**
func getExifData(filePath string) ([]byte, error) {
	parser := jpegstructure.NewJpegMediaParser()
	intfc, err := parser.ParseFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("EXIF情報なし")
	}

	// セグメントリストを取得
	sl := intfc.(*jpegstructure.SegmentList)
	segments := sl.Segments()

	// EXIFセグメントを探して、そのデータを返す
	for _, segment := range segments {
		if segment.MarkerId == jpegstructure.MARKER_APP1 {
			return segment.Data, nil
		}
	}

	return nil, fmt.Errorf("EXIFデータなし")
}

// **EXIFデータを保持してJPEGを保存（EXIFがない場合はそのまま保存）**
func saveImageWithExif(img image.Image, targetPath string, exifData []byte, quality int) {
	// メモリ上にJPEGをエンコード
	buf := new(bytes.Buffer)
	options := &jpeg.Options{Quality: quality}
	err := jpeg.Encode(buf, img, options)
	if err != nil {
		log.Printf("JPEGエンコードエラー: %v\n", err)
		return
	}

	// **EXIFデータがない場合はそのまま書き込み**
	if exifData == nil {
		outFile, err := os.Create(targetPath)
		if err != nil {
			log.Printf("保存エラー: %v\n", err)
			return
		}
		defer outFile.Close()

		_, err = io.Copy(outFile, buf)
		if err != nil {
			log.Printf("JPEG書き込みエラー: %v\n", err)
		}
		return
	}

	// **EXIFデータを埋め込む**
	parser := jpegstructure.NewJpegMediaParser()
	intfc, err := parser.ParseBytes(buf.Bytes())
	if err != nil {
		log.Printf("JPEGパースエラー: %v\n", err)
		return
	}

	// セグメントリストを取得
	sl := intfc.(*jpegstructure.SegmentList)

	// APP1セグメントとしてEXIFを追加
	segment := &jpegstructure.Segment{
		MarkerId: jpegstructure.MARKER_APP1,
		Data:     append([]byte("Exif\x00\x00"), exifData...),
	}
	sl.Add(segment)

	// 出力ファイルを作成
	outFile, err := os.Create(targetPath)
	if err != nil {
		log.Printf("保存エラー: %v\n", err)
		return
	}
	defer outFile.Close()

	// セグメントリストを書き込む
	err = sl.Write(outFile)
	if err != nil {
		log.Printf("JPEGの書き込みエラー: %v\n", err)
	}
}

// **画像のリサイズ**
func resizeImage(img image.Image) (image.Image, int) {

	bounds := img.Bounds()
	width := bounds.Max.X
	scale := 1.0
	quality := 95

	for {
		// リサイズ
		newWidth := uint(float64(width) * scale)
		resized := resize.Resize(newWidth, 0, img, resize.Lanczos3)

		// 一時ファイルに保存してサイズをチェック
		buf := new(bytes.Buffer)
		err := jpeg.Encode(buf, resized, &jpeg.Options{Quality: quality})
		if err != nil {
			log.Printf("エンコードエラー: %v\n", err)
			break
		}

		currentSize := int64(buf.Len())
		if currentSize <= lowerLimit {
			return resized, quality
		}

		// サイズに応じて圧縮強度を調整
		sizeRatio := float64(currentSize) / float64(lowerLimit)
		if sizeRatio > 5.0 {
			scale *= 0.5
			quality = 70
		} else if sizeRatio > 2.0 {
			if quality > 60 {
				quality -= 20
			} else {
				scale *= 0.6
			}
		} else {
			if quality > 30 {
				quality -= 10
			} else {
				scale *= 0.8
			}
		}

		// 限界に達した場合
		if scale < 0.05 || quality < 15 {
			break
		}
	}

	return img, 95
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

func ResizeImage(srcPath string, dstPath string, maxSize int64) error {
	// 入力ファイルを開く
	file, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("ファイルを開けません: %v", err)
	}
	defer file.Close()

	// 画像をデコード
	img, _, err := image.Decode(file)
	if err != nil {
		return fmt.Errorf("画像のデコードに失敗: %v", err)
	}

	// 元のサイズを取得
	bounds := img.Bounds()
	width := bounds.Max.X
	height := bounds.Max.Y

	scale := 1.0
	quality := 95

	for {
		// 現在のスケールでリサイズ
		newWidth := int(float64(width) * scale)
		newHeight := int(float64(height) * scale)
		resized := resize.Resize(uint(newWidth), uint(newHeight), img, resize.Lanczos3)

		// 一時ファイルを作成
		tempFile, err := os.CreateTemp(os.TempDir(), "resize_*.jpg")
		if err != nil {
			return fmt.Errorf("一時ファイルの作成に失敗: %v", err)
		}
		tempPath := tempFile.Name()

		// JPEGとして保存
		err = jpeg.Encode(tempFile, resized, &jpeg.Options{Quality: quality})
		tempFile.Close()

		if err != nil {
			os.Remove(tempPath)
			return fmt.Errorf("画像の保存に失敗: %v", err)
		}

		// サイズを確認
		fileInfo, err := os.Stat(tempPath)
		if err != nil {
			os.Remove(tempPath)
			return fmt.Errorf("ファイルサイズの取得に失敗: %v", err)
		}

		currentSize := fileInfo.Size()
		sizeRatio := float64(currentSize) / float64(maxSize)

		log.Printf("試行: scale=%.2f, quality=%d, size=%.2fMB (目標: %.2fMB, 比率: %.1f)",
			scale, quality, float64(currentSize)/1024/1024, float64(maxSize)/1024/1024, sizeRatio)

		if currentSize <= maxSize {
			// 目標達成
			err = os.Rename(tempPath, dstPath)
			if err != nil {
				os.Remove(tempPath)
				return fmt.Errorf("ファイルの移動に失敗: %v", err)
			}
			return nil
		}

		os.Remove(tempPath)

		// サイズに応じて圧縮強度を調整
		if sizeRatio > 5.0 {
			// サイズが目標の5倍以上
			scale *= 0.5
			quality = 70
		} else if sizeRatio > 2.0 {
			// サイズが目標の2倍以上
			if quality > 60 {
				quality -= 20
			} else {
				scale *= 0.6
			}
		} else {
			// サイズが目標の2倍未満
			if quality > 30 {
				quality -= 10
			} else {
				scale *= 0.8
			}
		}

		// 限界に達した場合
		if scale < 0.05 || quality < 15 {
			return fmt.Errorf("目標サイズ(%.2fMB)を達成できませんでした - 現在のサイズ: %.2fMB",
				float64(maxSize)/1024/1024, float64(currentSize)/1024/1024)
		}
	}
}

// processImageWithProgress は進捗状況付きで画像を処理
func processImageWithProgress(file os.DirEntry, currentCount, totalCount int) {
	sourcePath := filepath.Join(sourceDir, file.Name())
	targetPath := filepath.Join(targetDir, file.Name())

	// EXIFデータを取得（なくてもエラーを出さずに処理を続ける）
	exifData, err := getExifData(sourcePath)
	if err != nil {
		log.Printf("EXIF情報なし: %s（通常処理を継続） (%d/%d)\n", file.Name(), currentCount, totalCount)
	} else {
		log.Printf("EXIF情報取得成功: %s (%d/%d)\n", file.Name(), currentCount, totalCount)
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
	resizedImg, quality := resizeImage(img)

	// EXIFデータがある場合は保持、ない場合はそのまま保存
	saveImageWithExif(resizedImg, targetPath, exifData, quality)
}
