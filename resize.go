package main

import (
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/nfnt/resize"
)

var source_dir string = ""
var resize_option = ""

func main() {
	// 入力をパース
	flag.Parse()

	// 対象ディレクトリを引数から取得(string)
	source_dir = flag.Arg(0)

	// リサイズのオプションが指定された場合はセット
	resize_option = flag.Arg(1)

	// 対象ディレクトリの中の全ファイルを取得([]os.FileInfo)
	files, err := getFiles(source_dir)

	if err != nil {
		log.Fatal(err)
		panic(err)
	}

	// 取得したファイルからJPEGファイルのみを抽出([]os.FileInfo)
	jpeg_files := extractJpegImages(files)

	// サイズが20MB以上なら20MB未満になるまで解像度を落とす
	resizeJpegFiles(jpeg_files)
}

// used by main()
func getFiles(source_dir string) ([]os.FileInfo, error) {
	// 対象ディレクトリの中のファイル全てを取得、格納
	files, err := ioutil.ReadDir(source_dir)

	// エラーがあれば"err"を返す
	if err != nil {
		return nil, err
	}

	return files, nil
}

// used by main()
func extractJpegImages(files []os.FileInfo) []os.FileInfo {
	var jpeg_images []os.FileInfo
	for _, file := range files {
		switch filepath.Ext(file.Name()) {
		case ".jpeg", ".jpg", ".JPG":
			jpeg_images = append(jpeg_images, file)
		default:
		}
	}

	return jpeg_images
}

// used by decodeImage()
func conversionToReader(jpeg_file os.FileInfo) (io.Reader, error) {
	io_file, err := os.Open(source_dir + "/" + jpeg_file.Name())
	if err != nil {
		return nil, err
	}
	return io_file, nil
}

// used by resizeJpegFiles()
func decodeImage(jpeg_file os.FileInfo) (image.Image, error) {
	io_file, err := conversionToReader(jpeg_file)
	if err != nil {
		return nil, err
	}

	decoded_image, _, err := image.Decode(io_file)
	if err != nil {
		return nil, err
	}
	return decoded_image, nil
}

// used by resizeJpegFiles()
func encodeImage(resized_image image.Image, file_name string) error {
	output, err := os.Create(source_dir + "/" + file_name)
	if err != nil {
		return err
	}

	defer output.Close()

	opts := &jpeg.Options{Quality: 100}
	if err := jpeg.Encode(output, resized_image, opts); err != nil {
		return err
	}

	return nil
}

// used by main()
func resizeJpegFiles(jpeg_files []os.FileInfo) {

	for _, jpeg_file := range jpeg_files {
		// ファイルを画像として読み込み
		decoded_image, _ := decodeImage(jpeg_file)
		// 画像の横幅を取得
		image_width := float64(decoded_image.Bounds().Dx())

		// リサイズのオプションが指定されていたら幅を0.9倍
		if resize_option != "" {
			image_width *= 0.9
		}

		var resized_image image.Image

		if jpeg_file.Size() >= 20000000 {
			// 画像のリサイズ
			resized_image = resize.Resize(uint(image_width), 0, decoded_image, resize.Lanczos3)

			if resized_image != nil {
				// リサイズした画像を書き込み
				encodeImage(resized_image, jpeg_file.Name())
			}
		}
	}

	fmt.Println("resize completed!!")
}
