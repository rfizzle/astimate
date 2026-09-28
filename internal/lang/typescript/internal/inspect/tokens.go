package inspect

import (
	"fmt"

	"github.com/pkoukk/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"
)

// o200kCounter counts tokens exactly with the o200k_base encoding.
type o200kCounter struct {
	enc *tiktoken.Tiktoken
}

// newO200kCounter returns a counter using the o200k_base encoding, loaded
// from the BPE file embedded in tiktoken-go-loader, so it makes no network
// calls. tiktoken-go only accepts a loader through the package-level
// tiktoken.SetBpeLoader, so this sets it, as the Go extractor does; the
// library caches the parsed encoding process-wide.
func newO200kCounter() (*o200kCounter, error) {
	tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		return nil, fmt.Errorf("loading o200k_base encoding: %w", err)
	}
	return &o200kCounter{enc: enc}, nil
}

// count returns the o200k_base token count of src. Special-token text is
// counted as ordinary text.
func (o *o200kCounter) count(src []byte) int {
	return len(o.enc.EncodeOrdinary(string(src)))
}
