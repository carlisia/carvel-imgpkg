// Copyright 2020 VMware, Inc.
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"

	"github.com/cppforlife/go-cli-ui/ui"
	"github.com/spf13/cobra"
)

const (
	Version = "0.7.0"
)

type VersionOptions struct {
	ui ui.UI
}

func NewVersionOptions(ui ui.UI) *VersionOptions {
	return &VersionOptions{ui}
}

func NewVersionCmd(o *VersionOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print client version",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
	}
	return cmd
}

func (o *VersionOptions) Run() error {
	o.ui.PrintBlock([]byte(fmt.Sprintf("imgpkg version %s\n", Version)))

	return nil
}
