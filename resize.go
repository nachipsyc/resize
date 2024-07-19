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
	"strconv"

	"github.com/nfnt/resize"
)

// 使用する変数を定義
var source_dir string = ""
var target_dir string = ""
var lower_limit int64
var magnification float64

func main() {
	// 入力をパース
	flag.Parse()

	// 読み込み元ディレクトリのパスを引数から取得(string)
	source_dir = flag.Arg(0)

	// 書き出し先ディレクトリのパスを引数から取得(string)
	target_dir = flag.Arg(1)

	// 対象となる画像のファイルサイズの下限を引数から取得(int)
	lower_limit, _ = strconv.ParseInt(flag.Arg(2), 10, 64)

	// リサイズのオプションが指定された場合はセット(float)
	magnification, _ = strconv.ParseFloat(flag.Arg(3), 64)

	// 対象ディレクトリの中の全ファイルを取得([]os.FileInfo)
	files, err := getFiles(source_dir)

	// エラーがある場合は出力
	if err != nil {
		log.Fatal(err)
		panic(err)
	}

	// 取得したファイルからJPEGファイルのみを抽出([]os.FileInfo)
	jpeg_files := extractJpegFiles(files)

	// 画像を書き出し(サイズが指定した値以上のものはリサイズ処理を行う)
	resizeJpegFiles(jpeg_files)

	// encodeImages()として割り出し
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
func extractJpegFiles(files []os.FileInfo) []os.FileInfo {
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

// used by main()
func resizeJpegFiles(jpeg_files []os.FileInfo) {

	for _, jpeg_file := range jpeg_files {

		var resized_image image.Image

		// 指定したサイズよりも大きかった場合
		if jpeg_file.Size() >= lower_limit {

			// リザイズ対象画像の情報をプリント
			fmt.Print(jpeg_file.Name())
			fmt.Println(jpeg_file.Size())

			// ファイルを画像として読み込み
			decoded_image, _ := decodeImage(jpeg_file)

			// 画像の横幅を取得
			image_width := float64(decoded_image.Bounds().Dx())

			// リサイズの倍率が指定されていたら幅を変更
			if magnification != 0 {
				image_width *= float64(magnification)
			}

			// 画像のリサイズ
			resized_image = resize.Resize(uint(image_width), 0, decoded_image, resize.Lanczos3)
		}

		if resized_image != nil {
			// リサイズした画像を書き込み
			encodeImage(resized_image, jpeg_file.Name())
		}
		// else if (decoded_image != nil) {
		// 	// デコードした画像を書き込み
		// 	encodeImage(decoded_image, jpeg_file.Name())
		// }
	}

	fmt.Println("resize completed!!")
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
	output, err := os.Create(target_dir + "/" + file_name)
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

// used by decodeImage()
func conversionToReader(jpeg_file os.FileInfo) (io.Reader, error) {
	io_file, err := os.Open(source_dir + "/" + jpeg_file.Name())
	if err != nil {
		return nil, err
	}
	return io_file, nil
}
