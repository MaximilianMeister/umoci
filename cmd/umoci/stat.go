/*
 * umoci: Umoci Modifies Open Containers' Images
 * Copyright (C) 2016 SUSE LLC.
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

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/cyphar/umoci/image/cas"
	"github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/urfave/cli"
	"golang.org/x/net/context"
)

var statCommand = cli.Command{
	Name:  "stat",
	Usage: "displays status information of an image manifest",
	ArgsUsage: `--image <image-path>[:<tag>]

Where "<image-path>" is the path to the OCI image, and "<tag>" is the name of
the tagged image to stat.

WARNING: Do not depend on the output of this tool unless you're using --json.
The intention of the default formatting of this tool is that it is easy for
humans to read, and might change in future versions.`,

	// stat gives information about a manifest.
	Category: "image",

	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "json",
			Usage: "output the stat information as a JSON encoded blob",
		},
	},

	Action: stat,
}

func stat(ctx *cli.Context) error {
	imagePath := ctx.App.Metadata["layout"].(string)
	tagName := ctx.App.Metadata["tag"].(string)

	// Get a reference to the CAS.
	engine, err := cas.Open(imagePath)
	if err != nil {
		return err
	}
	defer engine.Close()

	manifestDescriptor, err := engine.GetReference(context.TODO(), tagName)
	if err != nil {
		return err
	}

	// FIXME: Implement support for manifest lists.
	if manifestDescriptor.MediaType != v1.MediaTypeImageManifest {
		return fmt.Errorf("--from descriptor does not point to v1.MediaTypeImageManifest: not implemented: %s", manifestDescriptor.MediaType)
	}

	// Get stat information.
	ms, err := Stat(context.TODO(), engine, *manifestDescriptor)
	if err != nil {
		return err
	}

	// Output the stat information.
	if ctx.Bool("json") {
		// Use JSON.
		if err := json.NewEncoder(os.Stdout).Encode(ms); err != nil {
			return err
		}
	} else {
		if err := ms.Format(os.Stdout); err != nil {
			return err
		}
	}

	return nil
}
