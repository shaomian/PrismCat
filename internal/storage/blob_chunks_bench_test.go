package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// The optional DB is read-only; only aggregate benchmark metrics are printed.
// All experimental files are written beneath b.TempDir, never into that DB/store.
func BenchmarkBlobStorageDataset(b *testing.B) {
	var bodies [][]byte
	if path := os.Getenv("PRISMCAT_BLOB_BENCH_DB"); path != "" {
		db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
		if err != nil {
			b.Fatal(err)
		}
		defer db.Close()
		rows, err := db.Query("SELECT request_body FROM request_logs ORDER BY created_at_unix_ms DESC LIMIT 100")
		if err != nil {
			b.Fatal(err)
		}
		var total int
		for rows.Next() {
			var body string
			if err = rows.Scan(&body); err != nil {
				b.Fatal(err)
			}
			if total+len(body) > 256<<20 {
				break
			}
			bodies = append(bodies, []byte(body))
			total += len(body)
		}
		if err = rows.Close(); err != nil {
			b.Fatal(err)
		}
	} else {
		base := contextBody(2 << 20)
		for i := 1; i <= 24; i++ {
			bodies = append(bodies, bytes.Clone(base[:i*len(base)/24]))
		}
	}
	for _, mode := range []string{"raw", "default", "zstd", "cdc-zstd"} {
		b.Run(mode, func(b *testing.B) {
			enc, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(1), zstd.WithWindowSize(chunkMaxSize))
			defer enc.Close()
			dec, _ := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
			defer dec.Close()
			var input, physical int64
			var writeTime, readTime time.Duration
			for range b.N {
				store, err := NewFileBlobStoreWithOptions(b.TempDir(), FileBlobStoreOptions{Chunked: mode == "cdc-zstd"})
				if err != nil {
					b.Fatal(err)
				}
				refs := make([]string, 0, len(bodies))
				start := time.Now()
				for _, body := range bodies {
					input += int64(len(body))
					var ref string
					if mode == "cdc-zstd" || mode == "default" {
						ref, err = store.Put(context.Background(), body)
					} else {
						sum := sha256.Sum256(body)
						ref = newSHA256Ref(sum)
						_, h, _ := parseBlobRef(ref)
						data := body
						if _, statErr := os.Stat(store.pathFor(h)); os.IsNotExist(statErr) {
							if mode == "zstd" {
								data = enc.EncodeAll(body, nil)
							}
							err = writeBlobFile(store.pathFor(h), data, false)
						}
					}
					if err != nil {
						b.Fatal(err)
					}
					refs = append(refs, ref)
				}
				writeTime += time.Since(start)
				start = time.Now()
				for i, ref := range refs {
					var data []byte
					if mode == "cdc-zstd" || mode == "default" {
						data, err = store.Get(context.Background(), ref)
					} else {
						_, h, _ := parseBlobRef(ref)
						data, err = os.ReadFile(store.pathFor(h))
						if err == nil && mode == "zstd" {
							data, err = dec.DecodeAll(data, nil)
						}
					}
					if err != nil || !bytes.Equal(data, bodies[i]) {
						b.Fatalf("%s round trip: %v", mode, err)
					}
				}
				readTime += time.Since(start)
				if err = filepath.WalkDir(store.baseDir, func(path string, d fs.DirEntry, err error) error {
					if err == nil && !d.IsDir() {
						info, statErr := d.Info()
						if statErr != nil {
							return statErr
						}
						physical += info.Size()
					}
					return err
				}); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(input)/float64(b.N), "input-B")
			b.ReportMetric(float64(physical)/float64(b.N), "stored-B")
			b.ReportMetric(float64(writeTime.Microseconds())/1000/float64(b.N), "write-ms")
			b.ReportMetric(float64(readTime.Microseconds())/1000/float64(b.N), "read-ms")
			b.ReportMetric(float64(physical)*100/float64(max(input, 1)), "stored-percent")
		})
	}
}
