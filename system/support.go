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

// SupportInfoFile is a support bundle the unit has generated.
type SupportInfoFile struct {
	File   string `json:"file"`
	MD5Sum string `json:"md5sum"`
	Size   int64  `json:"size"`
}

// GenerateSupportInfo has the unit build a support bundle (logs and
// configuration) for download with DownloadSupportInfo. It can take up to two
// minutes.
func GenerateSupportInfo(ctx context.Context, c *amplifi.Client) (*SupportInfoFile, error) {
	var f SupportInfoFile
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "GenerateSupportInfo", nil, &f); err != nil {
		return nil, fmt.Errorf("generate support info: %w", err)
	}
	return &f, nil
}

// DownloadSupportInfo copies a generated support bundle to w, chunk by
// chunk, and verifies its MD5 checksum. Each chunk arrives as a "data"
// string; it is taken to be base64 and used as is when it is not.
func DownloadSupportInfo(ctx context.Context, c *amplifi.Client, f *SupportInfoFile, w io.Writer) error {
	sum := md5.New() //nolint:gosec // matching the unit's checksum
	out := io.MultiWriter(w, sum)

	var offset int64
	for offset < f.Size {
		var res struct {
			Data string `json:"data"`
		}
		arg := map[string]any{"file": f.File, "offset": offset}
		if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "DownloadSupportInfo", arg, &res); err != nil {
			return fmt.Errorf("download support info at offset %d: %w", offset, err)
		}
		chunk := supportChunk(res.Data)
		if len(chunk) == 0 {
			return fmt.Errorf("download support info: empty chunk at offset %d of %d", offset, f.Size)
		}
		if _, err := out.Write(chunk); err != nil {
			return fmt.Errorf("download support info: %w", err)
		}
		offset += int64(len(chunk))
	}

	if f.MD5Sum == "" {
		return nil
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != f.MD5Sum {
		return fmt.Errorf("download support info: checksum mismatch: got %s, want %s", got, f.MD5Sum)
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
