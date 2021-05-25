package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gwaylib/errors"
)

const (
	append_file_new       = 0
	append_file_continue  = 1
	append_file_completed = 2
	pieceSize             = 32 * 1024
)

func checksumFile(aFile, bFile *os.File) (int, error) {
	if _, err := aFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}

	if _, err := bFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, bFile.Name())
	}

	//checksum all data
	ah := sha1.New()
	if _, err := io.Copy(ah, aFile); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}

	asum := ah.Sum(nil)

	bh := sha1.New()
	if _, err := io.Copy(bh, bFile); err != nil {
		return append_file_new, errors.As(err, bFile.Name())
	}

	bsum := bh.Sum(nil)
	if !bytes.Equal(asum, bsum) {
		return append_file_new, nil
	}
	return append_file_completed, nil
}

func canAppendFile(aFile, bFile *os.File, aStat, bStat os.FileInfo) (int, error) {
	checksumSize := int64(pieceSize) * 2
	// for small size, just do rewrite.
	aSize := aStat.Size()
	bSize := bStat.Size()
	if bSize < checksumSize {
		return append_file_new, nil
	}
	if bSize > aSize {
		return append_file_new, nil
	}

	aData := make([]byte, checksumSize)
	bData := make([]byte, checksumSize)
	// TODO: get random data
	if _, err := aFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	if _, err := bFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, bFile.Name())
	}

	if _, err := aFile.ReadAt(aData, bSize-checksumSize); err != nil {
		return append_file_new, errors.As(err)
	}
	if _, err := bFile.ReadAt(bData, bSize-checksumSize); err != nil {
		return append_file_new, errors.As(err)
	}
	if !bytes.Equal(aData, bData) {
		return append_file_new, nil
	}
	if aSize > bSize {
		return append_file_continue, nil
	}
	return append_file_completed, nil
}

func travelFile(path string) (os.FileInfo, []string, error) {
	fStat, err := os.Lstat(path) //todo
	if err != nil {
		return nil, nil, errors.As(err, path)
	}
	if !fStat.IsDir() {
		return nil, []string{path}, nil
	}
	dirs, err := os.ReadDir(path)
	if err != nil {
		return nil, nil, errors.As(err)
	}
	result := []string{}
	for _, fs := range dirs {
		filePath := filepath.Join(path, fs.Name())
		if !fs.IsDir() {
			result = append(result, filePath)
			continue
		}
		_, nextFiles, err := travelFile(filePath)
		if err != nil {
			return nil, nil, errors.As(err, filePath)
		}
		result = append(result, nextFiles...)
	}
	return fStat, result, nil
}

func copyFile(ctx context.Context, from, to string) error {
	if from == to {
		return errors.New("Same file").As(from, to)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		return errors.As(err, to)
	}
	fromFile, err := os.Open(from)
	if err != nil {
		return errors.As(err, from)
	}
	defer fromFile.Close()
	fromStat, err := fromFile.Stat()
	if err != nil {
		return errors.As(err, from)
	}

	// TODO: make chtime
	toFile, err := os.OpenFile(to, os.O_RDWR|os.O_CREATE, fromStat.Mode())
	if err != nil {
		return errors.As(err, to, uint64(fromStat.Mode()))
	}
	if err := toFile.Truncate(0); err != nil {
		return errors.As(err)
	}
	defer toFile.Close()
	toStat, err := toFile.Stat()
	if err != nil {
		return errors.As(err, to)
	}

	// checking continue
	stats, err := canAppendFile(fromFile, toFile, fromStat, toStat)
	if err != nil {
		return errors.As(err)
	}
	switch stats {
	case append_file_completed:
		// has done
		if _, err := checksumFile(fromFile, toFile); err != nil {
			return errors.As(err, fromFile)
		}
		fmt.Printf("%s ======= completed\n", to)
		return nil

	case append_file_continue:
		appendPos := int64(toStat.Size() - 1)
		if appendPos < 0 {
			appendPos = 0
			fmt.Printf("%s ====== new \n", to)
		} else {
			fmt.Printf("%s ====== continue: %d\n", to, appendPos)
		}
		if _, err := fromFile.Seek(appendPos, 0); err != nil {
			return errors.As(err)
		}
		if _, err := toFile.Seek(appendPos, 0); err != nil {
			return errors.As(err)
		}
	default:
		fmt.Printf("%s ====== new \n", to)
		if err := toFile.Truncate(0); err != nil {
			return errors.As(err)
		}
		if _, err := fromFile.Seek(0, 0); err != nil {
			return errors.As(err, from)
		}
		if _, err := toFile.Seek(0, 0); err != nil {
			return errors.As(err, to)
		}
	}

	errBuff := make(chan error, 1)
	interrupt := false
	iLock := sync.Mutex{}
	go func() {
		buf := make([]byte, pieceSize)
		for {
			iLock.Lock()
			if interrupt {
				iLock.Unlock()
				return
			}
			iLock.Unlock()

			nr, er := fromFile.Read(buf)
			if nr > 0 {
				nw, ew := toFile.Write(buf[0:nr])
				if ew != nil {
					errBuff <- errors.As(ew)
					return
				}
				if nr != nw {
					errBuff <- errors.As(io.ErrShortWrite)
					return
				}
			}
			if er != nil {
				errBuff <- errors.As(er)
				return
			}
		}
	}()
	select {
	case err := <-errBuff:
		if !errors.Equal(err, io.EOF) {
			return errors.As(err)
		}

		// TODO: checksum transfer data
		fromStat, err = fromFile.Stat()
		if err != nil {
			return err
		}
		toStat, err = toFile.Stat()
		if err != nil {
			return err
		}

		if fromStat.Size() != toStat.Size() {
			return errors.New("final size not match").As(from, to, fromStat.Size(), toStat.Size())
		}
		// if _, err := checksumFile(fromFile, toFile); err != nil {
		// 	return errors.As(err, "check sum failed")
		// }
		return nil
	case <-ctx.Done():
		iLock.Lock()
		interrupt = true
		iLock.Unlock()
		return ctx.Err()
	}
}

func CopyFile(ctx context.Context, from, to string) error {
	_, source, err := travelFile(from)
	if err != nil {
		return errors.As(err)
	}
	for _, src := range source {
		toFile := strings.Replace(src, from, to, 1)
		tCtx, cancel := context.WithTimeout(ctx, time.Hour)
		if err := copyFile(tCtx, src, toFile); err != nil {
			cancel()
			// do retry
			tCtx, cancel = context.WithTimeout(ctx, time.Hour)
			if err := copyFile(tCtx, src, toFile); err != nil {
				log.Warn(errors.As(err))

				cancel()
				return errors.As(err)
			}
		}
		cancel()
	}
	return nil
}

// func RsyncFile(ctx context.Context, from, to string) error {
// 	_, source, err := travelFile(from)
// 	if err != nil {
// 		return errors.As(err)
// 	}

// 	for _, src := range source {
// 		toFile := strings.Replace(src, from, to, 1)
// 		tCtx, cancel := context.WithTimeout(ctx, time.Minute*30)
// 		if err := rsyncFile(tCtx, src, toFile); err != nil {
// 			log.Warn(err)
// 			cancel()

// 			tCtx, cancel = context.WithTimeout(ctx, time.Minute*30)
// 			if err := rsyncFile(tCtx, src, toFile); err != nil {
// 				log.Warn(errors.As(err))

// 				cancel()
// 				return errors.As(err)
// 			}
// 		}
// 		cancel()
// 	}
// 	return nil
// }

// func rsyncFile(ctx context.Context, from, to string) error {
// 	if from == to {
// 		log.Warn(errors.New("Same file").As(from, to))
// 		return nil
// 	}
// 	now := time.Now()
// 	fmt.Printf("=========================new\n%s\n", to)
// 	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
// 		return errors.As(err, to)
// 	}

// 	_, err := exec.CommandContext(ctx, "rsync", "-Pat", from, to).CombinedOutput()
// 	if err != nil {
// 		fmt.Printf("Duration: %s\n", time.Since(now))
// 		fmt.Printf("=====================failed\n")
// 		return errors.As(err, to)
// 	}
// 	fmt.Printf("Duration: %s\n", time.Since(now))

// 	// fromFile, err := os.Open(from)
// 	// if err != nil {
// 	// 	return errors.As(err, from)
// 	// }

// 	// toFile, err := os.Open(to)
// 	// if err != nil {
// 	// 	return errors.As(err, from)
// 	// }
// 	// if _, err := checksumFile(fromFile, toFile); err != nil {
// 	// 	fmt.Printf("checksum file failed:%s\n", err)
// 	// 	return err
// 	// } else {
// 	// 	fmt.Printf("checksum file %s success\n", to)
// 	// }

// 	fmt.Printf("============================completed\n")
// 	return nil
// }

// func mvFile(ctx context.Context, from, to string) error {
// 	if from == to {
// 		log.Warn(errors.New("Same file").As(from, to))
// 	}

// 	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
// 		return errors.As(err, to)
// 	}
// 	if _, err := exec.CommandContext(ctx, "mv", from, to).CombinedOutput(); err != nil {
// 		return errors.As(err, to)
// 	}

// 	return nil
// }
