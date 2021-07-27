// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"os"
	"path/filepath"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/mit-dci/utreexo/accumulator"
	"github.com/mit-dci/utreexo/util"
)

const (
	// utreexoDirName is the name of the directory in which the utreexo state
	// is stored.
	utreexoDirName             = "utreexostate"
	defaultUtreexoFileName     = "forest.dat"
	defaultUtreexoMiscFileName = "miscforestfile.dat"
	defaultUtreexoCowDirName   = "cowstate"
	defaultUtreexoCowFileName  = "CURRENT"
	defaultCowMaxCache         = 1000
)

var (
	// TODO ugly global. But to get rid of this we need to change how the
	// restores and inits work in the utreexo repo.
	globalBasePath = ""
)

// UtreexoType specifies the utreexo state backend type. Each of the types
// are explained in the package accumulator.
type UtreexoType uint8

const (
	Ram UtreexoType = iota
	Cached
	Cow
	Disk
)

// UtreexoConfig is a descriptor which specifies the Utreexo state instance configuration.
type UtreexoConfig struct {
	// DataDir is the base path of where all the data for this node will be stored.
	// Utreexo has custom storage method and that data will be stored under this
	// directory.
	DataDir string

	// Type specifies what type of UtreexoBackEnd should be created.
	Type accumulator.ForestType

	// Params are the Bitcoin network parameters. This is used to separately store
	// different accumulators.
	Params *chaincfg.Params
}

// InitUtreexoState returns an initialized utreexo state. If there isn't an
// existing state on disk, it creates one and returns it.
func InitUtreexoState(cfg *UtreexoConfig) (*accumulator.Forest, error) {
	basePath := filepath.Join(cfg.DataDir, utreexoDirName)
	globalBasePath = basePath

	log.Infof("Initializing Utreexo state from '%s'", basePath)

	var forest *accumulator.Forest
	var err error
	if checkUtreexoExists(cfg, basePath) {
		forest, err = restoreUtreexoState(cfg, basePath)
		if err != nil {
			return nil, err
		}
	} else {
		forest, err = createUtreexoState(cfg, basePath)
		if err != nil {
			return nil, err
		}
	}

	log.Info("Utreexo state loaded")

	return forest, nil
}

// deleteUtreexoState removes the utreexo state directory and all the contents
// in it.
func deleteUtreexoState(dataDir string) error {
	basePath := filepath.Join(dataDir, utreexoDirName)
	_, err := os.Stat(basePath)
	if err == nil {
		log.Infof("Deleting the utreexo state at directory %s", basePath)
	} else {
		log.Infof("No utreexo state to delete")
	}
	return os.RemoveAll(basePath)
}

// checkUtreexoExists checks that the data for this utreexo state type specified
// in the config is present and should be resumed off of.
func checkUtreexoExists(cfg *UtreexoConfig, basePath string) bool {
	var path string
	switch cfg.Type {
	case accumulator.CowForest:
		cowPath := filepath.Join(basePath, defaultUtreexoCowDirName)
		path = filepath.Join(cowPath, defaultUtreexoCowFileName)
	default:
		path = filepath.Join(basePath, defaultUtreexoFileName)
	}

	return util.HasAccess(path)
}

// FlushUtreexoState saves the utreexo state to disk.
func (idx *UtreexoProofIndex) FlushUtreexoState() error {
	if _, err := os.Stat(globalBasePath); err != nil {
		os.MkdirAll(globalBasePath, os.ModePerm)
	}
	forestFilePath := filepath.Join(globalBasePath, defaultUtreexoFileName)
	forestFile, err := os.OpenFile(forestFilePath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	err = idx.utreexoView.WriteForestToDisk(forestFile, true, false)
	if err != nil {
		return err
	}

	miscFilePath := filepath.Join(globalBasePath, defaultUtreexoMiscFileName)
	// write other misc forest data
	miscForestFile, err := os.OpenFile(
		miscFilePath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	err = idx.utreexoView.WriteMiscData(miscForestFile)
	if err != nil {
		return err
	}

	return nil
}

// restoreUtreexoState restores forest fields based off the existing utreexo state
// on disk.
func restoreUtreexoState(cfg *UtreexoConfig, basePath string) (
	*accumulator.Forest, error) {

	var forest *accumulator.Forest

	switch cfg.Type {
	case accumulator.CowForest:
		cowPath := filepath.Join(basePath, defaultUtreexoCowDirName)
		miscForestFilePath := filepath.Join(basePath, defaultUtreexoMiscFileName)

		// Where the misc forest data exists
		miscForestFile, err := os.OpenFile(
			miscForestFilePath, os.O_RDONLY, 0400)
		if err != nil {
			return nil, err
		}
		forest, err = accumulator.RestoreForest(
			miscForestFile, nil, false, false, cowPath, defaultCowMaxCache)
		if err != nil {
			return nil, err
		}

	default:
		var (
			inRam bool
			cache bool
		)
		switch cfg.Type {
		case accumulator.RamForest:
			inRam = true
		case accumulator.CacheForest:
			cache = true
		}

		forestFilePath := filepath.Join(basePath, defaultUtreexoFileName)

		// Where the forestfile exists
		forestFile, err := os.OpenFile(forestFilePath, os.O_RDWR, 0400)
		if err != nil {
			return nil, err
		}
		miscFilePath := filepath.Join(basePath, defaultUtreexoMiscFileName)
		// Where the misc forest data exists
		miscForestFile, err := os.OpenFile(miscFilePath, os.O_RDONLY, 0400)
		if err != nil {
			return nil, err
		}

		forest, err = accumulator.RestoreForest(
			miscForestFile, forestFile, inRam, cache, "", 0)
		if err != nil {
			return nil, err
		}

	}

	return forest, nil
}

// createUtreexoState creates a new utreexo state and returns it.
func createUtreexoState(cfg *UtreexoConfig, basePath string) (
	*accumulator.Forest, error) {

	var forest *accumulator.Forest
	switch cfg.Type {
	case accumulator.RamForest:
		forest = accumulator.NewForest(cfg.Type, nil, "", 0)
	case accumulator.CowForest:
		// Default to 1000MB of cache for now.
		forest = accumulator.NewForest(cfg.Type, nil, basePath, 1000)
	default:
		forestFileName := filepath.Join(basePath, defaultUtreexoFileName)

		// Where the forestfile exists
		forestFile, err := os.OpenFile(
			forestFileName, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}

		// Restores all the forest data
		forest = accumulator.NewForest(cfg.Type, forestFile, "", 0)
	}

	return forest, nil
}
