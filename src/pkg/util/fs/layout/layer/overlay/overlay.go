// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package overlay

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/singularityware/singularity/src/pkg/util/fs"
	"github.com/singularityware/singularity/src/pkg/util/fs/layout"
	"github.com/singularityware/singularity/src/pkg/util/fs/mount"
)

const lowerDir = "/overlay-lowerdir"

// Overlay layer manager
type Overlay struct {
	session   *layout.Session
	lowerDirs []string
	upperDir  string
	workDir   string
}

// New creates and returns an overlay layer manager
func New() *Overlay {
	return &Overlay{}
}

// Add adds required directory in session layout
func (o *Overlay) Add(session *layout.Session, system *mount.System) error {
	o.session = session
	if err := o.session.AddDir(lowerDir); err != nil {
		return err
	}
	if o.lowerDirs == nil {
		o.lowerDirs = make([]string, 0)
	}
	path, _ := o.session.GetPath(lowerDir)
	o.lowerDirs = append(o.lowerDirs, path)

	if err := system.RunBeforeTag(mount.LayerTag, o.createOverlay); err != nil {
		return err
	}
	return nil
}

func (o *Overlay) createOverlay(system *mount.System) error {
	for _, point := range system.Points.GetByTag(mount.PreLayerTag) {
		switch point.Type {
		case "ext3":
			if o.upperDir != "" {
				return fmt.Errorf("there is already a writable overlay image")
			}
			u := point.Destination + "/upper"
			w := point.Destination + "/work"
			if fs.IsLink(u) {
				return fmt.Errorf("symlink detected, upper overlay %s must be a directory", u)
			}
			if fs.IsLink(w) {
				return fmt.Errorf("symlink detected, work overlay %s must be a directory", w)
			}
			if !fs.IsDir(u) {
				if err := os.Mkdir(u, 0755); err != nil {
					return fmt.Errorf("failed to create %s directory: %s", u, err)
				}
			}
			if !fs.IsDir(w) {
				if err := os.Mkdir(w, 0755); err != nil {
					return fmt.Errorf("failed to create %s directory: %s", w, err)
				}
			}
			o.upperDir = u
			o.workDir = w
		case "squashfs":
			o.AddLowerDir(point.Destination)
		default:
			o.AddLowerDir(point.Destination)
		}
	}
	o.lowerDirs = append(o.lowerDirs, o.session.RootFsPath())

	lowerdir := strings.Join(o.lowerDirs, ":")
	err := system.Points.AddOverlay(mount.LayerTag, o.session.FinalPath(), 0, lowerdir, o.upperDir, o.workDir)
	if err != nil {
		return err
	}

	points := system.Points.GetByTag(mount.RootfsTag)
	if len(points) <= 0 {
		return fmt.Errorf("no root fs image found")
	}
	return o.createLayer(points[0].Destination, system)
}

// AddLowerDir adds a lower directory to overlay mount
func (o *Overlay) AddLowerDir(path string) error {
	o.lowerDirs = append([]string{path}, o.lowerDirs...)
	return nil
}

// AddUpperDir adds upper directory to overlay mount
func (o *Overlay) AddUpperDir(path string) error {
	if o.upperDir != "" {
		return fmt.Errorf("upper directory was already set")
	}
	o.upperDir = path
	return nil
}

// AddWorkDir adds work directory to overlay mount
func (o *Overlay) AddWorkDir(path string) error {
	if o.workDir != "" {
		return fmt.Errorf("upper directory was already set")
	}
	o.workDir = path
	return nil
}

// createLayer creates overlay layer based on content of root filesystem
// given by rootFsPath
func (o *Overlay) createLayer(rootFsPath string, system *mount.System) error {
	sessionDir := o.session.Path()
	st := new(syscall.Stat_t)

	if sessionDir == "" {
		return fmt.Errorf("can't determine session path")
	}
	for _, tag := range mount.GetTagList() {
		for _, point := range system.Points.GetByTag(tag) {
			flags, _ := mount.ConvertOptions(point.Options)
			if flags&syscall.MS_REMOUNT != 0 {
				continue
			}
			if strings.HasPrefix(point.Destination, sessionDir) {
				continue
			}
			p := rootFsPath + point.Destination
			if syscall.Stat(p, st) == nil {
				continue
			}
			if err := syscall.Stat(point.Source, st); os.IsNotExist(err) {
				return fmt.Errorf("stat failed for %s: %s", point.Source, err)
			}
			dest := lowerDir + point.Destination
			// don't exist create it in overlay
			switch st.Mode & syscall.S_IFMT {
			case syscall.S_IFDIR:
				if err := o.session.AddDir(dest); err != nil {
					return err
				}
			default:
				if err := o.session.AddFile(dest, nil); err != nil {
					return err
				}
			}
		}
	}
	return o.session.Update()
}
