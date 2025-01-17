// Copyright 2020 VMware, Inc.
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cppforlife/go-cli-ui/ui"
	regname "github.com/google/go-containerregistry/pkg/name"
	regv1 "github.com/google/go-containerregistry/pkg/v1"
	ctlimg "github.com/k14s/imgpkg/pkg/imgpkg/image"
	"github.com/k14s/imgpkg/pkg/imgpkg/plainimage"
)

const (
	ImgpkgDir      = ".imgpkg"
	BundlesDir     = "bundles"
	ImagesLockFile = "images.yml"
)

type Contents struct {
	paths         []string
	excludedPaths []string
}

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 . ImagesMetadataWriter
type ImagesMetadataWriter interface {
	ctlimg.ImagesMetadata
	WriteImage(regname.Reference, regv1.Image) error
}

func NewContents(paths []string, excludedPaths []string) Contents {
	return Contents{paths: paths, excludedPaths: excludedPaths}
}

func (b Contents) Push(uploadRef regname.Tag, registry ImagesMetadataWriter, ui ui.UI) (string, error) {
	err := b.validate()
	if err != nil {
		return "", err
	}

	labels := map[string]string{BundleConfigLabel: "true"}
	return plainimage.NewContents(b.paths, b.excludedPaths).Push(uploadRef, labels, registry, ui)
}

func (b Contents) PresentsAsBundle() (bool, error) {
	imgpkgDirs, err := b.findImgpkgDirs()
	if err != nil {
		return false, err
	}

	err = b.validateImgpkgDirs(imgpkgDirs)
	if _, ok := err.(bundleValidationError); ok {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

func (b Contents) validate() error {
	imgpkgDirs, err := b.findImgpkgDirs()
	if err != nil {
		return err
	}

	err = b.validateImgpkgDirs(imgpkgDirs)
	if err != nil {
		return err
	}

	return nil
}

func (b *Contents) findImgpkgDirs() ([]string, error) {
	var bundlePaths []string
	for _, path := range b.paths {
		err := filepath.Walk(path, func(currPath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if filepath.Base(currPath) != ImgpkgDir {
				return nil
			}

			currPath, err = filepath.Abs(currPath)
			if err != nil {
				return err
			}

			bundlePaths = append(bundlePaths, currPath)

			return nil
		})

		if err != nil {
			return []string{}, err
		}
	}

	return bundlePaths, nil
}

func (b Contents) validateImgpkgDirs(imgpkgDirs []string) error {
	if len(imgpkgDirs) != 1 {
		return bundleValidationError{
			fmt.Sprintf("Expected one '%s' dir, got %d: %s",
				ImgpkgDir, len(imgpkgDirs), strings.Join(imgpkgDirs, ", "))}
	}

	path := imgpkgDirs[0]

	// make sure it is a child of one input dir
	for _, flagPath := range b.paths {
		flagPath, err := filepath.Abs(flagPath)
		if err != nil {
			return err
		}

		if filepath.Dir(path) == flagPath {
			return nil
		}
	}

	return bundleValidationError{
		fmt.Sprintf("Expected '%s' directory, to be a direct child of one of: %s; was %s",
			ImgpkgDir, strings.Join(b.paths, ", "), path)}
}

type bundleValidationError struct {
	msg string
}

func (b bundleValidationError) Error() string {
	return b.msg
}
