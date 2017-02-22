/*
 * umoci: Umoci Modifies Open Containers' Images
 * Copyright (C) 2016, 2017 SUSE LLC.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package mutate

import (
	"archive/tar"
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/openSUSE/umoci/oci/cas"
	imeta "github.com/opencontainers/image-spec/specs-go"
	ispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/net/context"

	// Include all known drivers.
	_ "github.com/openSUSE/umoci/oci/cas/drivers"
)

// These come from just running the code.
const (
	expectedLayerDigest    = "sha256:9a98de6b2015d531559791e60518fd376ddc62d3062ee4f691b223c06175dbef"
	expectedConfigDigest   = "sha256:908705c0f681cd2a69225ce302aa7bfe52fca02ac1ff29318e285be03ceb9123"
	expectedManifestDigest = "sha256:a42c4536afbed929a7539d1c89a079ec4e24f7f157b309322ce3dabdc2bbcf32"
)

func setup(t *testing.T, dir string) (cas.Engine, ispec.Descriptor) {
	dir = filepath.Join(dir, "image")
	if err := cas.Create(dir); err != nil {
		t.Fatal(err)
	}

	engine, err := cas.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Write a tar layer.
	var buffer bytes.Buffer
	tw := tar.NewWriter(&buffer)
	data := []byte("some contents")
	tw.WriteHeader(&tar.Header{
		Name:     "test",
		Mode:     0644,
		Typeflag: tar.TypeRegA,
		Size:     int64(len(data)),
	})
	tw.Write(data)
	tw.Close()

	// Push the base layer.
	diffidDigester := cas.BlobAlgorithm.Digester()
	hashReader := io.TeeReader(&buffer, diffidDigester.Hash())
	layerDigest, layerSize, err := engine.PutBlob(context.Background(), hashReader)
	if err != nil {
		t.Fatal(err)
	}
	if layerDigest != expectedLayerDigest {
		t.Errorf("unexpected layerDigest: got %s, expected %s", layerDigest, expectedLayerDigest)
	}

	// Create a config.
	config := ispec.Image{
		Config: ispec.ImageConfig{
			User: "default:user",
		},
		RootFS: ispec.RootFS{
			Type:    "layers",
			DiffIDs: []string{diffidDigester.Digest().String()},
		},
		History: []ispec.History{
			{EmptyLayer: false},
		},
	}

	configDigest, configSize, err := engine.PutBlobJSON(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if configDigest != expectedConfigDigest {
		t.Errorf("unexpected configDigest: got %s, expected %s", configDigest, expectedConfigDigest)
	}

	// Create the manifest.
	manifest := ispec.Manifest{
		Versioned: imeta.Versioned{
			SchemaVersion: 2,
		},
		Config: ispec.Descriptor{
			MediaType: ispec.MediaTypeImageConfig,
			Digest:    configDigest,
			Size:      configSize,
		},
		Layers: []ispec.Descriptor{
			{
				MediaType: ispec.MediaTypeImageLayerGzip,
				Digest:    layerDigest,
				Size:      layerSize,
			},
		},
	}

	manifestDigest, manifestSize, err := engine.PutBlobJSON(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	if manifestDigest != expectedManifestDigest {
		t.Errorf("unexpected manifestDigest: got %s, expected %s", manifestDigest, expectedManifestDigest)
	}

	return engine, ispec.Descriptor{
		MediaType: ispec.MediaTypeImageManifest,
		Digest:    manifestDigest,
		Size:      manifestSize,
	}
}

func TestMutateCache(t *testing.T) {
	dir, err := ioutil.TempDir("", "umoci-TestMutateBasic")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	engine, fromDescriptor := setup(t, dir)
	defer engine.Close()

	mutator, err := New(engine, fromDescriptor)
	if err != nil {
		t.Fatal(err)
	}

	// Check that caching actually works.
	if err := mutator.cache(context.Background()); err != nil {
		t.Fatalf("unexpected error getting cache: %+v", err)
	}

	// Check manifest.
	if mutator.manifest.SchemaVersion != 2 {
		t.Errorf("manifest.SchemaVersion is not cached")
	}
	if mutator.manifest.Config.MediaType != ispec.MediaTypeImageConfig {
		t.Errorf("manifest.Config.MediaType is not cached")
	}
	if mutator.manifest.Config.Digest != expectedConfigDigest {
		t.Errorf("manifest.Config.Digest is not cached")
	}
	if len(mutator.manifest.Layers) != 1 {
		t.Errorf("manifest.Layers is not cached")
	}
	if mutator.manifest.Layers[0].MediaType != ispec.MediaTypeImageLayerGzip {
		t.Errorf("manifest.Layers is not cached")
	}
	if mutator.manifest.Layers[0].Digest != expectedLayerDigest {
		t.Errorf("manifest.Layers.Digest is not cached")
	}

	// Check config.
	if mutator.config.Config.User != "default:user" {
		t.Errorf("config.Config.User is not cached")
	}
	if mutator.config.RootFS.Type != "layers" {
		t.Errorf("config.RootFS.Type is not cached")
	}
	if len(mutator.config.RootFS.DiffIDs) != 1 {
		t.Errorf("config.RootFS.DiffIDs is not cached")
	}
	// TODO: Check Config.RootFS.DiffIDs.Digest.
	if len(mutator.config.History) != 1 {
		t.Errorf("config.History is not cached")
	}
	if mutator.config.History[0].EmptyLayer != false {
		t.Errorf("config.History[0].EmptyLayer is not cached")
	}
}

func TestMutateAdd(t *testing.T) {
	dir, err := ioutil.TempDir("", "umoci-TestMutateAdd")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	engine, fromDescriptor := setup(t, dir)
	defer engine.Close()

	mutator, err := New(engine, fromDescriptor)
	if err != nil {
		t.Fatal(err)
	}

	// This isn't a valid image, but whatever.
	buffer := bytes.NewBufferString("contents")

	// Add a new layer.
	if err := mutator.Add(context.Background(), buffer, ispec.History{
		Comment: "new layer",
	}); err != nil {
		t.Fatalf("unexpected error adding layer: %+v", err)
	}

	newDescriptor, err := mutator.Commit(context.Background())
	if err != nil {
		t.Fatalf("unexpected error committing changes: %+v", err)
	}

	if newDescriptor.Digest == fromDescriptor.Digest {
		t.Fatalf("new and old descriptors are the same!")
	}

	mutator, err = New(engine, newDescriptor)
	if err != nil {
		t.Fatal(err)
	}

	// Cache the data to check it.
	if err := mutator.cache(context.Background()); err != nil {
		t.Fatalf("unexpected error getting cache: %+v", err)
	}

	// Check digests are different.
	if mutator.manifest.Config.Digest == expectedConfigDigest {
		t.Errorf("manifest.Config.Digest is the same!")
	}
	if mutator.manifest.Layers[0].Digest != expectedLayerDigest {
		t.Errorf("manifest.Layers[0].Digest is not the same!")
	}
	if mutator.manifest.Layers[1].Digest == expectedLayerDigest {
		t.Errorf("manifest.Layers[1].Digest is not the same!")
	}

	// Check layer was added.
	if len(mutator.manifest.Layers) != 2 {
		t.Errorf("manifest.Layers was not updated")
	}
	if mutator.manifest.Layers[1].MediaType != ispec.MediaTypeImageLayerGzip {
		t.Errorf("manifest.Layers[1].MediaType is the wrong value: %s", mutator.manifest.Layers[1].MediaType)
	}

	// Check config was also modified.
	if len(mutator.config.RootFS.DiffIDs) != 2 {
		t.Errorf("config.RootFS.DiffIDs was not updated")
	}

	// Check history.
	if len(mutator.config.History) != 2 {
		t.Errorf("config.History was not updated")
	}
	if mutator.config.History[1].EmptyLayer != false {
		t.Errorf("config.History[1].EmptyLayer was not set")
	}
	if mutator.config.History[1].Comment != "new layer" {
		t.Errorf("config.History[1].Comment was not set")
	}
}

func TestMutateAddNonDistributable(t *testing.T) {
	dir, err := ioutil.TempDir("", "umoci-TestMutateAddNonDistributable")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	engine, fromDescriptor := setup(t, dir)
	defer engine.Close()

	mutator, err := New(engine, fromDescriptor)
	if err != nil {
		t.Fatal(err)
	}

	// This isn't a valid image, but whatever.
	buffer := bytes.NewBufferString("contents")

	// Add a new layer.
	if err := mutator.AddNonDistributable(context.Background(), buffer, ispec.History{
		Comment: "new layer",
	}); err != nil {
		t.Fatalf("unexpected error adding layer: %+v", err)
	}

	newDescriptor, err := mutator.Commit(context.Background())
	if err != nil {
		t.Fatalf("unexpected error committing changes: %+v", err)
	}

	if newDescriptor.Digest == fromDescriptor.Digest {
		t.Fatalf("new and old descriptors are the same!")
	}

	mutator, err = New(engine, newDescriptor)
	if err != nil {
		t.Fatal(err)
	}

	// Cache the data to check it.
	if err := mutator.cache(context.Background()); err != nil {
		t.Fatalf("unexpected error getting cache: %+v", err)
	}

	// Check digests are different.
	if mutator.manifest.Config.Digest == expectedConfigDigest {
		t.Errorf("manifest.Config.Digest is the same!")
	}
	if mutator.manifest.Layers[0].Digest != expectedLayerDigest {
		t.Errorf("manifest.Layers[0].Digest is not the same!")
	}
	if mutator.manifest.Layers[1].Digest == expectedLayerDigest {
		t.Errorf("manifest.Layers[1].Digest is not the same!")
	}

	// Check layer was added.
	if len(mutator.manifest.Layers) != 2 {
		t.Errorf("manifest.Layers was not updated")
	}
	if mutator.manifest.Layers[1].MediaType != ispec.MediaTypeImageLayerNonDistributableGzip {
		t.Errorf("manifest.Layers[1].MediaType is the wrong value: %s", mutator.manifest.Layers[1].MediaType)
	}

	// Check config was also modified.
	if len(mutator.config.RootFS.DiffIDs) != 2 {
		t.Errorf("config.RootFS.DiffIDs was not updated")
	}

	// Check history.
	if len(mutator.config.History) != 2 {
		t.Errorf("config.History was not updated")
	}
	if mutator.config.History[1].EmptyLayer != false {
		t.Errorf("config.History[1].EmptyLayer was not set")
	}
	if mutator.config.History[1].Comment != "new layer" {
		t.Errorf("config.History[1].Comment was not set")
	}
}

func TestMutateSet(t *testing.T) {
	dir, err := ioutil.TempDir("", "umoci-TestMutateSet")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	engine, fromDescriptor := setup(t, dir)
	defer engine.Close()

	mutator, err := New(engine, fromDescriptor)
	if err != nil {
		t.Fatal(err)
	}

	// Add a new layer.
	if err := mutator.Set(context.Background(), ispec.ImageConfig{
		User: "changed:user",
	}, Meta{}, nil, ispec.History{
		Comment: "another layer",
	}); err != nil {
		t.Fatalf("unexpected error adding layer: %+v", err)
	}

	newDescriptor, err := mutator.Commit(context.Background())
	if err != nil {
		t.Fatalf("unexpected error committing changes: %+v", err)
	}

	if newDescriptor.Digest == fromDescriptor.Digest {
		t.Fatalf("new and old descriptors are the same!")
	}

	mutator, err = New(engine, newDescriptor)
	if err != nil {
		t.Fatal(err)
	}

	// Cache the data to check it.
	if err := mutator.cache(context.Background()); err != nil {
		t.Fatalf("unexpected error getting cache: %+v", err)
	}

	// Check digests are different.
	if mutator.manifest.Config.Digest == expectedConfigDigest {
		t.Errorf("manifest.Config.Digest is the same!")
	}

	// Check layer was not added.
	if len(mutator.manifest.Layers) != 1 {
		t.Errorf("manifest.Layers was updated")
	}

	// Check config was also modified.
	if len(mutator.config.RootFS.DiffIDs) != 1 {
		t.Errorf("config.RootFS.DiffIDs was updated")
	}
	if mutator.config.Config.User != "changed:user" {
		t.Errorf("config.Config.USer was not updated! expected changed:user, got %s", mutator.config.Config.User)
	}

	// Check history.
	if len(mutator.config.History) != 2 {
		t.Errorf("config.History was not updated")
	}
	if mutator.config.History[1].EmptyLayer != true {
		t.Errorf("config.History[1].EmptyLayer was not set")
	}
	if mutator.config.History[1].Comment != "another layer" {
		t.Errorf("config.History[1].Comment was not set")
	}
}
