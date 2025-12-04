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
	flag.Int64Var(&lowerLimit, "lower", 10000000, "ファイルサイズの下限")

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
	fmt.Printf("Completed. %d images resized\n", jpegCount)
}

// **元JPEGからAPP0〜APP15(メタデータ)セグメントをすべて抜き出す**
// EXIF(APP1), IPTC(APP13), XMP, ICC などをまとめて保持するための処理
func extractAppSegments(src []byte) []byte {
	// JPEG SOI チェック
	if len(src) < 4 || src[0] != 0xFF || src[1] != 0xD8 {
		return nil
	}

	buf := new(bytes.Buffer)
	i := 2 // SOI(FFD8)の直後から走査

	for {
		if i+4 > len(src) {
			break
		}
		if src[i] != 0xFF {
			// マーカーではない → ヘッダ部終了とみなす
			break
		}

		marker := src[i+1]

		// スキャン開始(SOS)に達したら終了
		if marker == 0xDA {
			break
		}

		// RST系やTEM以外は長さ付きセグメント
		if marker == 0xD8 || marker == 0xD9 {
			// SOI/EOI は2バイトだけ
			i += 2
			continue
		}

		if i+4 > len(src) {
			break
		}
		segLen := int(src[i+2])<<8 | int(src[i+3])
		segEnd := i + 2 + segLen
		if segEnd > len(src) {
			break
		}

		// APP0〜APP15 をそのまま退避
		if marker >= 0xE0 && marker <= 0xEF {
			buf.Write(src[i:segEnd])
		}

		i = segEnd
	}

	return buf.Bytes()
}

// **元画像のメタデータ(APPセグメント群)をすべて保持したままJPEGエンコード**
func encodeJpegWithMetadata(img image.Image, srcPath string, quality int) (*bytes.Buffer, error) {
	// まず通常のJPEGとしてエンコード
	encoded := new(bytes.Buffer)
	options := &jpeg.Options{Quality: quality}
	if err := jpeg.Encode(encoded, img, options); err != nil {
		return nil, fmt.Errorf("JPEGエンコードエラー: %w", err)
	}

	// 元ファイルを読み込んでメタデータ(APPセグメント)を抽出
	origBytes, err := os.ReadFile(srcPath)
	if err != nil {
		// 元が読めなければそのまま返す
		return encoded, nil
	}
	appSegs := extractAppSegments(origBytes)
	if len(appSegs) == 0 {
		// メタデータなし → そのまま返す
		return encoded, nil
	}

	// 新しいJPEGの先頭(SOI)直後にAPPセグメント群を差し込む
	encBytes := encoded.Bytes()
	if len(encBytes) < 2 || encBytes[0] != 0xFF || encBytes[1] != 0xD8 {
		// 想定外だが、一応そのまま返す
		return encoded, nil
	}

	out := new(bytes.Buffer)
	out.Write(encBytes[:2]) // SOI
	out.Write(appSegs)      // 元のメタデータ
	out.Write(encBytes[2:]) // 以降は新しくエンコードされたヘッダ＋画像データ

	return out, nil
}

// **メタデータ(EXIF, IPTC, TIFF など)を保持してJPEGを保存**
func saveImageWithMetadata(img image.Image, srcPath, targetPath string, quality int) {
	buf, err := encodeJpegWithMetadata(img, srcPath, quality)
	if err != nil {
		log.Printf("%v\n", err)
		return
	}

	outFile, err := os.Create(targetPath)
	if err != nil {
		log.Printf("保存エラー: %v\n", err)
		return
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, buf); err != nil {
		log.Printf("JPEG書き込みエラー: %v\n", err)
	}
}

// **画像のリサイズ**
func resizeImage(img image.Image) (image.Image, int) {

	bounds := img.Bounds()
	width := bounds.Max.X
	scale := 1.0
	quality := 100

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

		// 元画像のメタデータ(APPセグメント)をすべて保持したままJPEGをメモリ上にエンコード
		buf, err := encodeJpegWithMetadata(resized, srcPath, quality)
		if err != nil {
			return fmt.Errorf("画像の保存に失敗: %v", err)
		}

		// サイズを確認
		currentSize := int64(buf.Len())
		sizeRatio := float64(currentSize) / float64(maxSize)

		log.Printf("試行: scale=%.2f, quality=%d, size=%.2fMB (目標: %.2fMB, 比率: %.1f)",
			scale, quality, float64(currentSize)/1024/1024, float64(maxSize)/1024/1024, sizeRatio)

		if currentSize <= maxSize {
			// 目標達成 → 出力ファイルとして保存
			outFile, err := os.Create(dstPath)
			if err != nil {
				return fmt.Errorf("ファイルの作成に失敗: %v", err)
			}
			defer outFile.Close()

			if _, err := io.Copy(outFile, buf); err != nil {
				return fmt.Errorf("ファイルの書き込みに失敗: %v", err)
			}

			return nil
		}

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
	fileInfo, err := imgFile.Stat()
	if err != nil {
		log.Printf("ファイル情報取得エラー: %v\n", err)
		return
	}
	fileSize := fileInfo.Size()
	if fileSize < lowerLimit {
		// lowerLimit 未満ならそのままコピー
		copyFile(sourcePath, targetPath)
		return
	}

	// リサイズ処理
	resizedImg, quality := resizeImage(img)

	// 進捗をログに出力（currentCount / totalCount）
	log.Printf("Resizing %s (%d/%d)\n", file.Name(), currentCount, totalCount)

	// EXIF / IPTC / TIFF など、元JPEGのAPPセグメントをすべて保持して保存
	saveImageWithMetadata(resizedImg, sourcePath, targetPath, quality)
}
