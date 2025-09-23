// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package image_tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"

	"github.com/hashicorp/go-hclog"
)

var (
	ErrNoMemory = errors.New("invalid memory assignation")
)

type QemuTools struct {
	logger hclog.Logger
}

func NewHandler(logger hclog.Logger) *QemuTools {
	return &QemuTools{
		logger: logger,
	}
}

type ImageInfo struct {
	Format      string
	VirtualSize int64
}

// GetImageFormat runs `qemu-img info` to get the format of a disk image.
func (q *QemuTools) GetImageFormat(basePath string) (string, error) {
	info, err := q.GetImageInfo(basePath)
	if err != nil {
		return "", err
	}
	return info.Format, nil
}

// GetImageInfo runs `qemu-img info` to get the format and size of a disk image.
func (q *QemuTools) GetImageInfo(basePath string) (*ImageInfo, error) {
	q.logger.Debug("reading the disk format", "base", basePath)

	var stdoutBuf, stderrBuf bytes.Buffer

	cmd := exec.Command("qemu-img", "info", "-U", "--output=json", basePath)
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	if err != nil {
		q.logger.Error("qemu-img read image", "stderr", stderrBuf.String())
		q.logger.Debug("qemu-img read image", "stdout", stdoutBuf.String())
		return nil, err
	}

	q.logger.Debug("qemu-img read image", "stdout", stdoutBuf.String())

	// Parse the qemu-img info output to get format and size
	var output = struct {
		Format      string `json:"format"`
		VirtualSize int64  `json:"virtual-size"`
	}{}

	err = json.Unmarshal(stdoutBuf.Bytes(), &output)
	if err != nil {
		return nil, fmt.Errorf("qemu-img: unable read info response %s: %w", basePath, err)
	}

	return &ImageInfo{
		Format:      output.Format,
		VirtualSize: output.VirtualSize,
	}, nil
}

func (q *QemuTools) CreateThinCopy(basePath string, destination string, sizeM int64) error {
	q.logger.Debug("creating thin copy", "base", basePath, "dest", destination)

	var stdoutBuf, stderrBuf bytes.Buffer

	if sizeM <= 0 {
		return fmt.Errorf("qemu-img: %w", ErrNoMemory)
	}

	cmd := exec.Command("qemu-img", "create", "-b", basePath, "-f", "qcow2", "-F", "qcow2",
		destination, fmt.Sprintf("%dM", sizeM),
	)
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	if err != nil {
		q.logger.Error("qemu-img create output", "stderr", stderrBuf.String())
		q.logger.Debug("qemu-img create output", "stdout", stdoutBuf.String())
		return err
	}

	q.logger.Debug("qemu-img create output", "stdout", stdoutBuf.String())
	return nil
}
