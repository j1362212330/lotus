package main

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/gwaylib/errors"
)

// clean cache of unusing sector.
func (w *worker) CleanCache(ctx context.Context) error {
	w.workMu.Lock()
	defer w.workMu.Unlock()

	// not do this on miner repo
	if filepath.Base(w.workerRepo) == ".lotusstorage" {
		return nil
	}

	sealed := filepath.Join(w.workerRepo, "sealed")
	cache := filepath.Join(w.workerRepo, "cache")
	// staged := filepath.Join(w.workerRepo, "staging")
	unsealed := filepath.Join(w.workerRepo, "unsealed")
	if err := w.cleanCache(ctx, sealed); err != nil {
		return errors.As(err)
	}
	if err := w.cleanCache(ctx, cache); err != nil {
		return errors.As(err)
	}
	if err := w.cleanCache(ctx, unsealed); err != nil {
		return errors.As(err)
	}
	return nil
}

func (w *worker) cleanCache(ctx context.Context, path string) error {
	files, err := ioutil.ReadDir(path)
	if err != nil {
		log.Warn(errors.As(err))
	} else {
		fileNames := []string{}
		for _, f := range files {
			fileNames = append(fileNames, f.Name())
		}
		api, err := GetNodeApi()
		if err != nil {
			return errors.As(err)
		}
		ws, err := api.WorkerWorkingById(ctx, fileNames)
		if err != nil {
			ReleaseNodeApi(false)
			return errors.As(err, fileNames)
		}
	sealedLoop:
		for _, f := range files {
			for _, s := range ws {
				if s.ID == f.Name() {
					continue sealedLoop
				}
			}
			log.Infof("Remove %s", filepath.Join(path, f.Name()))
			if err := os.RemoveAll(filepath.Join(path, f.Name())); err != nil {
				return errors.As(err, w.workerCfg.IP, filepath.Join(path, f.Name()))
			}
		}
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return errors.As(err, w.workerCfg.IP)
	}
	return nil
}
