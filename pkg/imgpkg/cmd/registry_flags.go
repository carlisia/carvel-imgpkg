// Copyright 2020 VMware, Inc.
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"os"

	"github.com/k14s/imgpkg/pkg/imgpkg/registry"
	"github.com/spf13/cobra"
)

type RegistryFlags struct {
	CACertPaths []string
	VerifyCerts bool
	Insecure    bool

	Username string
	Password string
	Token    string
	Anon     bool
}

func (r *RegistryFlags) Set(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&r.CACertPaths, "registry-ca-cert-path", nil, "Add CA certificates for registry API (format: /tmp/foo) (can be specified multiple times)")
	cmd.Flags().BoolVar(&r.VerifyCerts, "registry-verify-certs", true, "Set whether to verify server's certificate chain and host name")
	cmd.Flags().BoolVar(&r.Insecure, "registry-insecure", false, "Allow the use of http when interacting with registries")

	cmd.Flags().StringVar(&r.Username, "registry-username", "", "Set username for auth ($IMGPKG_USERNAME)")
	cmd.Flags().StringVar(&r.Password, "registry-password", "", "Set password for auth ($IMGPKG_PASSWORD)")
	cmd.Flags().StringVar(&r.Token, "registry-token", "", "Set token for auth ($IMGPKG_TOKEN)")
	cmd.Flags().BoolVar(&r.Anon, "registry-anon", false, "Set anonymous auth ($IMGPKG_ANON)")
}

func (r *RegistryFlags) AsRegistryOpts() registry.Opts {
	opts := registry.Opts{
		CACertPaths: r.CACertPaths,
		VerifyCerts: r.VerifyCerts,
		Insecure:    r.Insecure,

		Username: r.Username,
		Password: r.Password,
		Token:    r.Token,
		Anon:     r.Anon,
	}

	if len(opts.Username) == 0 {
		opts.Username = os.Getenv("IMGPKG_USERNAME")
	}
	if len(opts.Password) == 0 {
		opts.Password = os.Getenv("IMGPKG_PASSWORD")
	}
	if len(opts.Token) == 0 {
		opts.Token = os.Getenv("IMGPKG_TOKEN")
	}
	if os.Getenv("IMGPKG_ANON") == "true" {
		opts.Anon = true
	}

	return opts
}
