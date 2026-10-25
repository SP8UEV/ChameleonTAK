package datapkg

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"chameleontak/internal/config"
)

// Every local file header must carry the real CRC and sizes (no data descriptor, flag bit 3),
// because ATAK reads packages with its own zip implementation.
func TestPackagesUseClassicZipHeaders(t *testing.T) {
	cfg := config.Default()
	cfg.ServerName = "192.168.1.102"
	for _, platform := range []string{"atak", "itak", "wintak"} {
		data, err := Connection(Params{
			Platform: platform, Config: cfg, Username: "jan", Callsign: "JAN",
			ClientP12: []byte("client-p12"), TrustP12: []byte("trust-p12"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if out := os.Getenv("DATAPKG_DUMP"); out != "" {
			_ = os.WriteFile(out+"-"+platform+".zip", data, 0o644)
		}
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range zr.File {
			off, err := f.DataOffset()
			if err != nil {
				t.Fatal(err)
			}
			hdr := data[off-int64(30+len(f.Name)+len(f.Extra)) : off]
			if binary.LittleEndian.Uint32(hdr[0:4]) != 0x04034b50 {
				t.Fatalf("%s/%s: local header not found", platform, f.Name)
			}
			flags := binary.LittleEndian.Uint16(hdr[6:8])
			size := binary.LittleEndian.Uint32(hdr[22:26])
			if flags&0x8 != 0 {
				t.Fatalf("%s/%s: uses a data descriptor", platform, f.Name)
			}
			if !f.FileInfo().IsDir() && uint64(size) != f.UncompressedSize64 {
				t.Fatalf("%s/%s: local header size %d, want %d", platform, f.Name, size, f.UncompressedSize64)
			}
			if len(f.Extra) != 0 {
				t.Fatalf("%s/%s: unexpected extra field", platform, f.Name)
			}
		}
	}
}
