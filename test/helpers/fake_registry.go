// Copyright 2020 VMware, Inc.
// SPDX-License-Identifier: Apache-2.0

package helpers

import (
	"archive/tar"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	regregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	regremote "github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/k14s/imgpkg/pkg/imgpkg/bundle"
	"github.com/k14s/imgpkg/pkg/imgpkg/image"
	"github.com/k14s/imgpkg/pkg/imgpkg/lockconfig"
	"github.com/k14s/imgpkg/pkg/imgpkg/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type FakeTestRegistryBuilder struct {
	images map[string]*ImageOrImageIndexWithTarPath
	server *httptest.Server
	t      *testing.T
	auth   authn.Authenticator
}

func NewFakeRegistry(t *testing.T) *FakeTestRegistryBuilder {
	r := &FakeTestRegistryBuilder{images: map[string]*ImageOrImageIndexWithTarPath{}, t: t}
	r.server = httptest.NewServer(regregistry.New(regregistry.Logger(log.New(io.Discard, "", 0))))

	return r
}

func (r *FakeTestRegistryBuilder) Build() registry.Registry {
	u, err := url.Parse(r.server.URL)
	assert.NoError(r.t, err)

	for imageRef, val := range r.images {
		imageRefWithTestRegistry, err := name.ParseReference(fmt.Sprintf("%s/%s", u.Host, imageRef))
		assert.NoError(r.t, err)
		auth := regremote.WithAuth(r.auth)

		if val.Image != nil {
			err = regremote.Write(imageRefWithTestRegistry, val.Image, regremote.WithNondistributable, auth)
			assert.NoError(r.t, err)
			err = regremote.Tag(imageRefWithTestRegistry.Context().Tag("latest"), val.Image, auth)
			assert.NoError(r.t, err)
		}

		if val.ImageIndex != nil {
			err = regremote.WriteIndex(imageRefWithTestRegistry, val.ImageIndex, regremote.WithNondistributable, auth)
			assert.NoError(r.t, err)
			err = regremote.Tag(imageRefWithTestRegistry.Context().Tag("latest"), val.ImageIndex, auth)
			assert.NoError(r.t, err)
		}
	}

	reg, err := registry.NewRegistry(registry.Opts{})
	assert.NoError(r.t, err)
	return reg
}

func (r *FakeTestRegistryBuilder) WithBasicAuth(username string, password string) {
	parentHandler := r.server.Config.Handler

	authenticatedRegistry := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.String(), "/v2/") {
			// In order to let ggcr know that this registry uses authentication, the /v2/ endpoint needs to return a
			// 'challenge' response when 'pinging' the /v2/ endpoint.
			writer.Header().Add("WWW-Authenticate", "Basic")
			writer.WriteHeader(401)
			return
		}

		usernameFromReq, passwordFromReq, ok := request.BasicAuth()
		if usernameFromReq != username || passwordFromReq != password || !ok {
			writer.WriteHeader(401)
			return
		}

		parentHandler.ServeHTTP(writer, request)
	})

	r.auth = &authn.Basic{
		Username: username,
		Password: password,
	}
	r.server.Config.Handler = authenticatedRegistry
}

func (r *FakeTestRegistryBuilder) WithIdentityToken(idToken string) {
	const accessToken string = "access_token"
	r.auth = &authn.Bearer{Token: accessToken}

	parentHandler := r.server.Config.Handler

	oauth2HandlerFunc := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.String(), "/v2/") {
			// In order to let ggcr know that this registry uses authentication, the /v2/ endpoint needs to return a
			// 'challenge' response when 'pinging' the /v2/ endpoint.

			writer.Header().Add("WWW-Authenticate", `Bearer service="fakeRegistry",realm="`+r.server.URL+`/id_token_auth"`)
			writer.WriteHeader(401)
			return
		}

		if strings.HasSuffix(request.URL.String(), "/id_token_auth") {
			requestBody, err := ioutil.ReadAll(request.Body)
			assert.NoError(r.t, err)
			if !strings.Contains(string(requestBody), "&refresh_token="+idToken) {
				writer.WriteHeader(401)
				return
			}
			_, _ = writer.Write([]byte(fmt.Sprintf(`{
						"access_token": "%s",
						"scope": "pubsub",
						"token_type": "bearer",
						"expires_in": 3600
					}`, accessToken)))
			return
		}

		if request.Header.Get("Authorization") != "Bearer "+accessToken {
			writer.WriteHeader(401)
			return
		}

		parentHandler.ServeHTTP(writer, request)
	})

	r.server.Config.Handler = oauth2HandlerFunc
}

func (r *FakeTestRegistryBuilder) WithRegistryToken(regToken string) {
	r.auth = &authn.Bearer{Token: regToken}

	parentHandler := r.server.Config.Handler

	authHandlerFunc := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.String(), "/v2/") {
			// In order to let ggcr know that this registry uses authentication, the /v2/ endpoint needs to return a
			// 'challenge' response when 'pinging' the /v2/ endpoint.

			writer.Header().Add("WWW-Authenticate", `Bearer realm="some.realm"`)
			writer.WriteHeader(401)
			return
		}

		if request.Header.Get("Authorization") != "Bearer "+regToken {
			writer.WriteHeader(401)
			return
		}

		parentHandler.ServeHTTP(writer, request)
	})

	r.server.Config.Handler = authHandlerFunc
}

func (r *FakeTestRegistryBuilder) WithBundleFromPath(bundleName string, path string) BundleInfo {
	tarballLayer, err := compress(path)
	require.NoError(r.t, err)
	label := map[string]string{"dev.carvel.imgpkg.bundle": ""}

	bundle, err := image.NewFileImage(tarballLayer.Name(), label)
	require.NoError(r.t, err)

	r.updateState(bundleName, bundle, nil, path)
	digest, err := bundle.Digest()
	assert.NoError(r.t, err)

	return BundleInfo{r, bundle, bundleName, path, digest.String(), r.ReferenceOnTestServer(bundleName + "@" + digest.String())}
}

func (r *FakeTestRegistryBuilder) WithRandomBundle(bundleName string) BundleInfo {
	bundle, err := random.Image(500, 5)
	require.NoError(r.t, err)

	bundle, err = mutate.ConfigFile(bundle, &v1.ConfigFile{
		Config: v1.Config{
			Labels: map[string]string{"dev.carvel.imgpkg.bundle": "true"},
		},
	})
	require.NoError(r.t, err, "create image from tar")

	r.updateState(bundleName, bundle, nil, "")

	digest, err := bundle.Digest()
	assert.NoError(r.t, err)

	return BundleInfo{r, bundle, bundleName, "", digest.String(), r.ReferenceOnTestServer(bundleName + "@" + digest.String())}
}

func (r *FakeTestRegistryBuilder) WithImageFromPath(imageNameFromTest string, path string, labels map[string]string) *ImageOrImageIndexWithTarPath {
	tarballLayer, err := compress(path)
	require.NoError(r.t, err)

	fileImage, err := image.NewFileImage(tarballLayer.Name(), labels)
	require.NoError(r.t, err)

	return r.updateState(imageNameFromTest, fileImage, nil, path)
}

func (r *FakeTestRegistryBuilder) WithRandomImage(imageNameFromTest string) *ImageOrImageIndexWithTarPath {
	img, err := random.Image(500, 3)
	require.NoError(r.t, err, "create image from tar")

	return r.updateState(imageNameFromTest, img, nil, "")
}

func (r *FakeTestRegistryBuilder) WithImage(imageNameFromTest string, image v1.Image) *ImageOrImageIndexWithTarPath {
	return r.updateState(imageNameFromTest, image, nil, "")
}

func (r *FakeTestRegistryBuilder) CopyImage(img ImageOrImageIndexWithTarPath, to string) *ImageOrImageIndexWithTarPath {
	return r.updateState(to, img.Image, nil, "")
}

func (r *FakeTestRegistryBuilder) CopyBundleImage(bundleInfo BundleInfo, to string) BundleInfo {
	newBundle := *r.images[bundleInfo.BundleName]
	r.updateState(to, newBundle.Image, nil, "")
	return BundleInfo{r, newBundle.Image, to, "", bundleInfo.Digest, bundleInfo.RefDigest}
}

func (r *FakeTestRegistryBuilder) WithARandomImageIndex(imageName string) *ImageOrImageIndexWithTarPath {
	index, err := random.Index(1024, 1, 1)
	require.NoError(r.t, err)

	return r.updateState(imageName, nil, index, "")
}

func (r *FakeTestRegistryBuilder) WithNonDistributableLayerInImage(imageNames ...string) {
	for _, imageName := range imageNames {
		layer, err := random.Layer(1024, types.OCIUncompressedRestrictedLayer)
		require.NoErrorf(r.t, err, "create layer: %s", imageName)

		imageWithARestrictedLayer, err := mutate.AppendLayers(r.images[imageName].Image, layer)
		require.NoErrorf(r.t, err, "add layer: %s", imageName)

		r.updateState(imageName, imageWithARestrictedLayer, r.images[imageName].ImageIndex, r.images[imageName].path)
	}
}

func (r *ImageOrImageIndexWithTarPath) WithNonDistributableLayer() *ImageOrImageIndexWithTarPath {
	layer, err := random.Layer(1024, types.OCIUncompressedRestrictedLayer)
	require.NoError(r.t, err)

	r.Image, err = mutate.AppendLayers(r.Image, layer)
	require.NoError(r.t, err)
	return r.fakeRegistry.updateState(r.RefDigest, r.Image, r.ImageIndex, r.path)
}

func (r *FakeTestRegistryBuilder) CleanUp() {
	for _, tarPath := range r.images {
		os.Remove(filepath.Join(tarPath.path, ".imgpkg", "images.yml"))
	}
	if r.server != nil {
		r.server.Close()
	}
}

func (r *FakeTestRegistryBuilder) ReferenceOnTestServer(repo string) string {
	u, err := url.Parse(r.server.URL)
	assert.NoError(r.t, err)
	return fmt.Sprintf("%s/%s", u.Host, repo)
}

func (r *FakeTestRegistryBuilder) Host() string {
	u, err := url.Parse(r.server.URL)
	assert.NoError(r.t, err)
	return u.Host
}

func (r *FakeTestRegistryBuilder) updateState(imageName string, image v1.Image, imageIndex v1.ImageIndex, path string) *ImageOrImageIndexWithTarPath {
	imgName, err := name.ParseReference(imageName)
	require.NoError(r.t, err)

	imageOrImageIndexWithTarPath := &ImageOrImageIndexWithTarPath{fakeRegistry: r, t: r.t, Image: image, ImageIndex: imageIndex, path: path}

	var digest v1.Hash
	if image != nil {
		digest, err = image.Digest()
		require.NoError(r.t, err)
	} else {
		digest, err = imageIndex.Digest()
		require.NoError(r.t, err)
	}

	imageOrImageIndexWithTarPath.RefDigest = r.ReferenceOnTestServer(imgName.Context().RepositoryStr() + "@" + digest.String())
	imageOrImageIndexWithTarPath.Digest = digest.String()
	r.images[imgName.Context().RepositoryStr()+"@"+digest.String()] = imageOrImageIndexWithTarPath
	r.images[imgName.Context().RepositoryStr()] = imageOrImageIndexWithTarPath

	return imageOrImageIndexWithTarPath
}

func (r *FakeTestRegistryBuilder) WithImageIndex(imageIndexName string, images ...mutate.Appendable) *ImageOrImageIndexWithTarPath {
	index, err := random.Index(500, 1, 1)
	assert.NoError(r.t, err)

	for _, image := range images {
		index = mutate.AppendManifests(index, mutate.IndexAddendum{
			Add: image,
		})
	}

	return r.updateState(imageIndexName, nil, index, "")
}

func (r *FakeTestRegistryBuilder) RemoveImage(imageRef string) {
	u, err := url.Parse(r.server.URL)
	assert.NoError(r.t, err)

	imageRefWithTestRegistry, err := name.ParseReference(fmt.Sprintf("%s/%s", u.Host, imageRef))
	assert.NoError(r.t, err)

	err = regremote.Delete(imageRefWithTestRegistry, regremote.WithAuth(r.auth))
	assert.NoError(r.t, err)
}

type BundleInfo struct {
	r          *FakeTestRegistryBuilder
	Image      v1.Image
	BundleName string
	BundlePath string
	Digest     string
	RefDigest  string
}

func (b BundleInfo) WithEveryImageFromPath(path string, labels map[string]string) BundleInfo {
	imgLockPath := filepath.Join(b.BundlePath, ".imgpkg", "images.yml.template")
	imgLock, err := lockconfig.NewImagesLockFromPath(imgLockPath)
	assert.NoError(b.r.t, err)

	var imageRefs []lockconfig.ImageRef
	imagesLock := lockconfig.ImagesLock{
		LockVersion: lockconfig.LockVersion{
			APIVersion: lockconfig.ImagesLockAPIVersion,
			Kind:       lockconfig.ImagesLockKind,
		},
	}

	for _, img := range imgLock.Images {
		imageFromPath := b.r.WithImageFromPath(img.Image, path, labels)
		imageRef, err := name.ParseReference(img.Image)
		assert.NoError(b.r.t, err)

		digest, err := imageFromPath.Image.Digest()
		assert.NoError(b.r.t, err)

		u, err := url.Parse(b.r.server.URL)
		assert.NoError(b.r.t, err)
		imageRefs = append(imageRefs, lockconfig.ImageRef{
			Image: u.Host + "/" + imageRef.Context().RepositoryStr() + "@" + digest.String(),
		})
	}

	imagesLock.Images = imageRefs
	imagesLockFile := filepath.Join(b.BundlePath, bundle.ImgpkgDir, bundle.ImagesLockFile)
	err = imagesLock.WriteToPath(imagesLockFile)
	assert.NoError(b.r.t, err)

	delete(b.r.images, b.BundleName+"@"+b.Digest)
	return b.r.WithBundleFromPath(b.BundleName, b.BundlePath)
}

func (b BundleInfo) WithImageRefs(imageRefs []lockconfig.ImageRef) BundleInfo {
	imagesLock := lockconfig.ImagesLock{
		LockVersion: lockconfig.LockVersion{
			APIVersion: lockconfig.ImagesLockAPIVersion,
			Kind:       lockconfig.ImagesLockKind,
		},
	}

	imagesLock.Images = imageRefs
	err := imagesLock.WriteToPath(filepath.Join(b.BundlePath, bundle.ImgpkgDir, bundle.ImagesLockFile))
	assert.NoError(b.r.t, err)

	delete(b.r.images, b.BundleName+"@"+b.Digest)
	return b.r.WithBundleFromPath(b.BundleName, b.BundlePath)
}

type ImageOrImageIndexWithTarPath struct {
	fakeRegistry *FakeTestRegistryBuilder
	Image        v1.Image
	ImageIndex   v1.ImageIndex
	path         string
	t            *testing.T
	RefDigest    string
	Digest       string
}

func compress(src string) (*os.File, error) {
	_, err := os.Stat(src)
	if err != nil {
		return nil, fmt.Errorf("Unable to compress because file not found: %s", err)
	}
	tempTarFile, err := ioutil.TempFile(os.TempDir(), "compressed-layer")
	if err != nil {
		return nil, err
	}
	tw := tar.NewWriter(tempTarFile)

	// walk through every file in the folder
	filepath.Walk(src, func(file string, fi os.FileInfo, _ error) error {
		header, err := tar.FileInfoHeader(fi, file)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, file)
		if err != nil {
			return err
		}

		header.Name = rel

		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if !fi.IsDir() {
			data, err := os.Open(file)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, data); err != nil {
				return err
			}
		}
		return nil
	})

	// produce tar
	if err := tw.Close(); err != nil {
		return tempTarFile, err
	}

	return tempTarFile, err
}
