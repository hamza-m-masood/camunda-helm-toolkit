package bundle

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
)

// WriteArchive packages files into a gzipped tar at path.
func WriteArchive(path string, files []File) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	for _, file := range files {
		hdr := &tar.Header{
			Name: file.Name,
			Mode: 0o644,
			Size: int64(len(file.Content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("writing header for %s: %w", file.Name, err)
		}
		if _, err := tw.Write(file.Content); err != nil {
			return fmt.Errorf("writing content for %s: %w", file.Name, err)
		}
	}
	return nil
}
