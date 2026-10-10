package system

import (
	"context"
	"crypto/md5" //nolint:gosec // the unit publishes an MD5 checksum
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// SupportBundle is a support bundle the unit has created.
type SupportBundle struct {
	File   string `json:"file"`
	MD5Sum string `json:"md5sum"`
	Size   int64  `json:"size"`
}

// CreateSupportBundle has the unit build a support bundle (logs and
// configuration) for download with DownloadSupportBundle and waits for it.
// It can take up to two minutes.
func CreateSupportBundle(ctx context.Context, c *amplifi.Client) (*SupportBundle, error) {
	var f SupportBundle
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "GenerateSupportInfo", nil, &f); err != nil {
		return nil, fmt.Errorf("create support bundle: %w", err)
	}
	return &f, nil
}

// DownloadSupportBundle copies a created support bundle to w, chunk by
// chunk, and verifies its MD5 checksum. Each chunk arrives as a "data"
// string; it is taken to be base64 and used as is when it is not.
func DownloadSupportBundle(ctx context.Context, c *amplifi.Client, f *SupportBundle, w io.Writer) error {
	sum := md5.New() //nolint:gosec // matching the unit's checksum
	out := io.MultiWriter(w, sum)

	var offset int64
	for offset < f.Size {
		var res struct {
			Data string `json:"data"`
		}
		arg := map[string]any{"file": f.File, "offset": offset}
		if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "DownloadSupportInfo", arg, &res); err != nil {
			return fmt.Errorf("download support bundle at offset %d: %w", offset, err)
		}
		chunk := supportChunk(res.Data)
		if len(chunk) == 0 {
			return fmt.Errorf("download support bundle: empty chunk at offset %d of %d", offset, f.Size)
		}
		if _, err := out.Write(chunk); err != nil {
			return fmt.Errorf("download support bundle: %w", err)
		}
		offset += int64(len(chunk))
	}

	if f.MD5Sum == "" {
		return nil
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != f.MD5Sum {
		return fmt.Errorf("download support bundle: checksum mismatch: got %s, want %s", got, f.MD5Sum)
	}
	return nil
}

// supportChunk decodes a DownloadSupportInfo chunk.
func supportChunk(data string) []byte {
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return []byte(data)
	}
	return b
}
