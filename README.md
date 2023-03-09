# About

- 特定のディレクトリ内の JPEG ファイルに対してまとめてリサイズ処理を行う

# Use

go run resize.go *1 *2 *3 *4

- \*1…ファイル取得元ディレクトリの相対パス
- \*2…書き出し先ディレクトリの相対パス
- \*3…リサイズ対象とするファイルのファイルサイズ下限閾値
  <br>
  （Byte 単位）
- \*4…リサイズの際の一辺の変更倍率
  <br>
  （省略可能、省略した場合は同じ長さで書き出しを行う）

## Sample

- Desktop にある"hoge"フォルダの中にあるファイルを、Desktop にある"resized_hoge"フォルダに書き出す
- 20MB 以上のファイルは幅が 0.9 倍されて（20MB 未満は幅変更なし）書き出しされる
  <br>
  $ cd fuga/fuga/resize
  <br>
  $ go run resize.go ../../../Desktop/hoge ../../../Desktop/resized_hoge 20000000 0.9
