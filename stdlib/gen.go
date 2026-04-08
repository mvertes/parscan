// Code generation directive for stdlib bindings.
// Run "GOROOT=$HOME/sdk/go1.26.1 go generate ./stdlib" to regenerate all binding files.

package stdlib

//go:generate go run ../cmd/extract -gen -o bufio.go bufio $GOROOT/src/bufio
//go:generate go run ../cmd/extract -gen -o bytes.go bytes $GOROOT/src/bytes
//go:generate go run ../cmd/extract -gen -o crypto_sha256.go crypto/sha256 $GOROOT/src/crypto/sha256
//go:generate go run ../cmd/extract -gen -o encoding_base64.go encoding/base64 $GOROOT/src/encoding/base64
//go:generate go run ../cmd/extract -gen -o encoding_csv.go encoding/csv $GOROOT/src/encoding/csv
//go:generate go run ../cmd/extract -gen -o encoding_hex.go encoding/hex $GOROOT/src/encoding/hex
//go:generate go run ../cmd/extract -gen -o encoding_json.go encoding/json $GOROOT/src/encoding/json
//go:generate go run ../cmd/extract -gen -o encoding_xml.go encoding/xml $GOROOT/src/encoding/xml
//go:generate go run ../cmd/extract -gen -o errors.go errors $GOROOT/src/errors
//go:generate go run ../cmd/extract -gen -o flag.go flag $GOROOT/src/flag
//go:generate go run ../cmd/extract -gen -o fmt.go fmt $GOROOT/src/fmt
//go:generate go run ../cmd/extract -gen -o hash_crc32.go hash/crc32 $GOROOT/src/hash/crc32
//go:generate go run ../cmd/extract -gen -o html.go html $GOROOT/src/html
//go:generate go run ../cmd/extract -gen -o html_template.go html/template $GOROOT/src/html/template
//go:generate go run ../cmd/extract -gen -o io.go io $GOROOT/src/io
//go:generate go run ../cmd/extract -gen -o io_fs.go io/fs $GOROOT/src/io/fs
//go:generate go run ../cmd/extract -gen -o log.go log $GOROOT/src/log
//go:generate go run ../cmd/extract -gen -o math.go math $GOROOT/src/math
//go:generate go run ../cmd/extract -gen -o math_bits.go math/bits $GOROOT/src/math/bits
//go:generate go run ../cmd/extract -gen -o math_rand_v2.go math/rand/v2 $GOROOT/src/math/rand/v2
//go:generate go run ../cmd/extract -gen -o net_url.go net/url $GOROOT/src/net/url
//go:generate go run ../cmd/extract -gen -o os_exec.go os/exec $GOROOT/src/os/exec
//go:generate go run ../cmd/extract -gen -o path.go path $GOROOT/src/path
//go:generate go run ../cmd/extract -gen -o path_filepath.go path/filepath $GOROOT/src/path/filepath
//go:generate go run ../cmd/extract -gen -o reflect.go reflect $GOROOT/src/reflect
//go:generate go run ../cmd/extract -gen -o regexp.go regexp $GOROOT/src/regexp
//go:generate go run ../cmd/extract -gen -o sort.go sort $GOROOT/src/sort
//go:generate go run ../cmd/extract -gen -o strconv.go strconv $GOROOT/src/strconv
//go:generate go run ../cmd/extract -gen -o strings.go strings $GOROOT/src/strings
//go:generate go run ../cmd/extract -gen -o sync.go sync $GOROOT/src/sync
//go:generate go run ../cmd/extract -gen -o text_tabwriter.go text/tabwriter $GOROOT/src/text/tabwriter
//go:generate go run ../cmd/extract -gen -o time.go time $GOROOT/src/time
//go:generate go run ../cmd/extract -gen -o unicode.go unicode $GOROOT/src/unicode
