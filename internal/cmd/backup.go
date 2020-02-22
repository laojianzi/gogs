// Copyright 2017 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	"github.com/unknwon/cae/zip"
	"github.com/unknwon/com"
	"github.com/urfave/cli"
	"gopkg.in/ini.v1"
	log "unknwon.dev/clog/v2"

	"gogs.io/gogs/internal/conf"
	"gogs.io/gogs/internal/db"
)

var Backup = cli.Command{
	Name:  "backup",
	Usage: "Backup files and database",
	Description: `Backup dumps and compresses all related files and database into zip file,
which can be used for migrating Gogs to another server. The output format is meant to be
portable among all supported database engines.`,
	Action: runBackup,
	Flags: []cli.Flag{
		stringFlag("config, c", "", "Custom configuration file path"),
		boolFlag("verbose, v", "Show process details"),
		stringFlag("tempdir, t", os.TempDir(), "Temporary directory path"),
		stringFlag("target", "./", "Target directory path to save backup archive"),
		stringFlag("archive-name", fmt.Sprintf("gogs-backup-%s.zip", time.Now().Format("20060102150405")), "Name of backup archive"),
		boolFlag("database-only", "Only dump database"),
		boolFlag("exclude-repos", "Exclude repositories"),
	},
}

const _CURRENT_BACKUP_FORMAT_VERSION = 1
const _ARCHIVE_ROOT_DIR = "gogs-backup"

func runBackup(c *cli.Context) error {
	zip.Verbose = c.Bool("verbose")

	err := conf.Init(c.String("config"))
	if err != nil {
		return errors.Wrap(err, "init configuration")
	}

	db.SetEngine()

	tmpDir := c.String("tempdir")
	if !com.IsExist(tmpDir) {
		log.Fatal("'--tempdir' does not exist: %s", tmpDir)
	}
	rootDir, err := ioutil.TempDir(tmpDir, "gogs-backup-")
	if err != nil {
		log.Fatal("Failed to create backup root directory '%s': %v", rootDir, err)
	}
	log.Info("Backup root directory: %s", rootDir)

	// Metadata
	metaFile := path.Join(rootDir, "metadata.ini")
	metadata := ini.Empty()
	metadata.Section("").Key("VERSION").SetValue(com.ToStr(_CURRENT_BACKUP_FORMAT_VERSION))
	metadata.Section("").Key("DATE_TIME").SetValue(time.Now().String())
	metadata.Section("").Key("GOGS_VERSION").SetValue(conf.App.Version)
	if err = metadata.SaveTo(metaFile); err != nil {
		log.Fatal("Failed to save metadata '%s': %v", metaFile, err)
	}

	archiveName := filepath.Join(c.String("target"), c.String("archive-name"))
	log.Info("Packing backup files to: %s", archiveName)

	z, err := zip.Create(archiveName)
	if err != nil {
		log.Fatal("Failed to create backup archive '%s': %v", archiveName, err)
	}
	if err = z.AddFile(_ARCHIVE_ROOT_DIR+"/metadata.ini", metaFile); err != nil {
		log.Fatal("Failed to include 'metadata.ini': %v", err)
	}

	// Database
	dbDir := filepath.Join(rootDir, "db")
	if err = db.DumpDatabase(dbDir); err != nil {
		log.Fatal("Failed to dump database: %v", err)
	}
	if err = z.AddDir(_ARCHIVE_ROOT_DIR+"/db", dbDir); err != nil {
		log.Fatal("Failed to include 'db': %v", err)
	}

	// Custom files
	if !c.Bool("database-only") {
		if err = z.AddDir(_ARCHIVE_ROOT_DIR+"/custom", conf.CustomDir()); err != nil {
			log.Fatal("Failed to include 'custom': %v", err)
		}
	}

	// Data files
	if !c.Bool("database-only") {
		for _, dir := range []string{"attachments", "avatars", "repo-avatars"} {
			dirPath := filepath.Join(conf.Server.AppDataPath, dir)
			if !com.IsDir(dirPath) {
				continue
			}

			if err = z.AddDir(path.Join(_ARCHIVE_ROOT_DIR+"/data", dir), dirPath); err != nil {
				log.Fatal("Failed to include 'data': %v", err)
			}
		}
	}

	// Repositories
	if !c.Bool("exclude-repos") && !c.Bool("database-only") {
		reposDump := filepath.Join(rootDir, "repositories.zip")
		log.Info("Dumping repositories in %q", conf.Repository.Root)
		if err = zip.PackTo(conf.Repository.Root, reposDump, true); err != nil {
			log.Fatal("Failed to dump repositories: %v", err)
		}
		log.Info("Repositories dumped to: %s", reposDump)

		if err = z.AddFile(_ARCHIVE_ROOT_DIR+"/repositories.zip", reposDump); err != nil {
			log.Fatal("Failed to include 'repositories.zip': %v", err)
		}
	}

	if err = z.Close(); err != nil {
		log.Fatal("Failed to save backup archive '%s': %v", archiveName, err)
	}

	os.RemoveAll(rootDir)
	log.Info("Backup succeed! Archive is located at: %s", archiveName)
	log.Stop()
	return nil
}
