# About

Resize photo files in specified directory at once.

# Use

go run resize.go *1 *2 *3 *4

*1…ファイル取得元ディレクトリの相対パス
*2…書き出し先ディレクトリの相対パス
*3…リサイズ対象とするファイルのファイルサイズ上限閾値
（Byte 単位）
*4…リサイズの際の一辺の変更倍率
（省略可能、省略した場合は同じ長さで書き出しを行う）
