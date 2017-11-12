package local_meta

import (
	"io"
	"net/url"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/vmihailenco/msgpack"
)

// Metadata constants describe the metadata available
// for a local Item.
const (
	MetadataPath       = "path"
	MetadataIsDir      = "is_dir"
	MetadataDir        = "dir"
	MetadataName       = "name"
	MetadataMode       = "mode"
	MetadataModeD      = "mode_d"
	MetadataPerm       = "perm"
	MetadataINode      = "inode"
	MetadataSize       = "size"
	MetadataIsHardlink = "is_hardlink"
	MetadataIsSymlink  = "is_symlink"
	MetadataLink       = "link"
)

type item struct {
	path     string // absolute path to file
	name     string // file name
	infoOnce sync.Once // protects info
	info     os.FileInfo
	infoErr  error
	metadata map[string]interface{}
	properties map[string]string
}

func (i *item) ID() string {
	return i.name
}

func (i *item) Name() string {
	return filepath.Base(i.path)
}

func (i *item) Size() (int64, error) {
	err := i.ensureInfo()
	if err != nil {
		return 0, err
	}
	return i.info.Size(), nil
}

func (i *item) URL() *url.URL {
	return &url.URL{
		Scheme: "file-meta",
		Path:   filepath.Clean(i.path),
	}
}

func (i *item) ETag() (string, error) {
	err := i.ensureInfo()
	if err != nil {
		return "", nil
	}

	if etag, ok := i.properties["Etag"]; ok {
		return etag, nil
	}

	return i.info.ModTime().String(), nil
}

// Open opens the file for reading.
func (i *item) Open() (io.ReadCloser, error) {
	r, err := os.Open(i.path)
	if err != nil {
		return nil, err
	}

	var bufMeta [3]byte
	_, err = io.ReadFull(r, bufMeta[:])
	if err != nil {
		return nil, err
	}

	if bytes.Compare(bufMeta[0:1], metaPointer[0:1]) == 0 {
		var metaLen [4]byte
		_, err := io.ReadFull(r, metaLen[:])
		if err != nil {
			return nil, err
		}
		mLen := binary.LittleEndian.Uint32(metaLen[:])

		var metaUnmarshall map[string]string
		metaUnmarshall = make(map[string]string)
		metaData := make([]byte, mLen)
		n, err := io.ReadFull(r, metaData)
		if err != nil {
			return nil, err
		}

		// compare file metadata version
		if bufMeta[2] == metaPointer[2] {
			if uint32(n) != mLen {
				return nil, errors.New("Invalid metadata")
			}

			err = msgpack.Unmarshal(metaData[:], &metaUnmarshall)
			if err != nil {
				return nil, err
			}

			if i.metadata == nil {
				i.metadata = make(map[string]interface{})
			}

			i.properties = metaUnmarshall
			for k, v := range i.properties {
				i.metadata[k] = v
			}
		}

	} else {
		r.Seek(0, 0)
	}

	return r, err
}

func (i *item) LastMod() (time.Time, error) {
	err := i.ensureInfo()
	if err != nil {
		return time.Time{}, err
	}

	if lastMod, ok := i.properties["Last-Modified"]; ok {
		lastModTime, err := time.Parse(http.TimeFormat, lastMod)
		if err != nil {
			return time.Time{}, err
		}

		return lastModTime, nil
	}

	return i.info.ModTime(), nil
}

func (i *item) ensureInfo() error {
	i.infoOnce.Do(func() {
		if i.info == nil || i.metadata == nil {
			i.info, i.infoErr = os.Lstat(i.path) // retrieve item file info

			i.infoErr = i.setMetadata(i.info) // merge file and metadata maps
			if i.infoErr != nil {
				return
			}

			r, err := i.Open()
			if err == nil {
				defer r.Close()
			} else {
				i.infoErr = err
				return
			}

		}

	})
	return i.infoErr
}

func (i *item) setMetadata(info os.FileInfo) error {
	fileMetadata := getFileMetadata(i.path, info) // retrieve file metadata
	i.metadata = fileMetadata
	return nil
}

// Metadata gets stat information for the file.
func (i *item) Metadata() (map[string]interface{}, error) {
	err := i.ensureInfo()
	if err != nil {
		return nil, err
	}


	return i.metadata, nil
}
