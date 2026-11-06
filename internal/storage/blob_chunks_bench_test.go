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
// Every mode syncs each new file and batches directory syncs per body. These are
// equally durable paths, not an estimate of the default buffered raw writer.
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
			bodies = append(bodies, bytes.Repeat([]byte("event: response.output_text.delta\ndata: {\"delta\":\"tool result\"}\n\n"), i*10))
		}
	}
	for _, mode := range []string{"raw-sync", "zstd-sync", "hybrid-sync"} {
		b.Run(mode, func(b *testing.B) {
			enc, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(1), zstd.WithWindowSize(chunkMaxSize))
			defer enc.Close()
			dec, _ := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
			defer dec.Close()
			var input, physical int64
			var writeTime, readTime time.Duration
			for range b.N {
				store, err := NewFileBlobStoreWithOptions(b.TempDir(), FileBlobStoreOptions{Chunked: mode == "hybrid-sync"})
				if err != nil {
					b.Fatal(err)
				}
				refs := make([]string, 0, len(bodies))
				start := time.Now()
				for _, body := range bodies {
					input += int64(len(body))
					var ref string
					if mode == "hybrid-sync" {
						ref, err = store.Put(context.Background(), body)
					} else {
						sum := sha256.Sum256(body)
						ref = newSHA256Ref(sum)
						_, h, _ := parseBlobRef(ref)
						data := body
						if _, statErr := os.Stat(store.pathFor(h)); os.IsNotExist(statErr) {
							if mode == "zstd-sync" {
								encoded := enc.EncodeAll(body, []byte{1})
								if len(encoded) < len(body)+1 {
									data = encoded
								} else {
									data = append([]byte{0}, body...)
								}
							}
							batch := newBlobWriteBatch(store.baseDir)
							if err = batch.write(store.pathFor(h), data); err == nil {
								err = batch.syncDirs()
							}
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
					if mode == "hybrid-sync" {
						data, err = store.Get(context.Background(), ref)
					} else {
						_, h, _ := parseBlobRef(ref)
						data, err = os.ReadFile(store.pathFor(h))
						if err == nil && mode == "zstd-sync" {
							if data[0] == 1 {
								data, err = dec.DecodeAll(data[1:], nil)
							} else {
								data = data[1:]
							}
						}
						if err == nil && newSHA256Ref(sha256.Sum256(data)) != ref {
							b.Fatal("whole-body integrity check failed")
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
