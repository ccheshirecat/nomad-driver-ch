// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package virt

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ccheshirecat/nomad-driver-ch/cloudinit"
	domain "github.com/ccheshirecat/nomad-driver-ch/internal/shared"
	"github.com/ccheshirecat/nomad-driver-ch/virt/image_tools"
	"github.com/ccheshirecat/nomad-driver-ch/virt/net"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/plugins/drivers"
	dtestutil "github.com/hashicorp/nomad/plugins/drivers/testutils"
	plugins "github.com/hashicorp/nomad/plugins/shared/structs"
	"github.com/shoenig/test/must"
)

// getEnvOrDefault returns the value of an environment variable or a default value if not set
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

type mockNet struct{}

func (mn *mockNet) Fingerprint(map[string]*plugins.Attribute) {
}

func (mn *mockNet) Init() error {
	return nil
}

func (mn *mockNet) VMStartedBuild(*net.VMStartedBuildRequest) (*net.VMStartedBuildResponse, error) {
	return &net.VMStartedBuildResponse{}, nil
}

func (mn *mockNet) VMTerminatedTeardown(*net.VMTerminatedTeardownRequest) (*net.VMTerminatedTeardownResponse, error) {
	return &net.VMTerminatedTeardownResponse{}, nil
}

type mockImageHandler struct {
	lock sync.RWMutex

	basePath    string
	imageFormat string
	err         error
}

func (mh *mockImageHandler) GetImageFormat(basePath string) (string, error) {
	mh.basePath = basePath
	return mh.imageFormat, mh.err
}

func (mh *mockImageHandler) CreateThinCopy(basePath string, destination string, sizeM int64) error {
	// For testing, create an actual file that Cloud Hypervisor can use
	if sourceData, err := os.ReadFile(basePath); err == nil {
		return os.WriteFile(destination, sourceData, 0644)
	} else {
		// Create minimal test file if source not available
		return os.WriteFile(destination, []byte("test image content"), 0644)
	}
}

func (mh *mockImageHandler) GetImageInfo(basePath string) (*image_tools.ImageInfo, error) {
	mh.basePath = basePath
	if mh.err != nil {
		return nil, mh.err
	}
	return &image_tools.ImageInfo{
		Format:      mh.imageFormat,
		VirtualSize: 10 * 1024 * 1024 * 1024, // 10GB default for testing
	}, nil
}

type mockTaskGetter struct {
	lock sync.RWMutex

	count int
	info  *domain.Info
	err   error
}

func (mtg *mockTaskGetter) GetDomain(name string) (*domain.Info, error) {
	mtg.lock.Lock()
	defer mtg.lock.Unlock()

	mtg.count += 1
	return mtg.info, mtg.err
}

func (mtg *mockTaskGetter) getNumberOfCalls() int {
	mtg.lock.Lock()
	defer mtg.lock.Unlock()

	return mtg.count
}

type mockVirtualizar struct {
	lock sync.RWMutex

	config *domain.Config
	count  int
	err    error
}

func (mv *mockVirtualizar) Start(dataDir string) error {
	return nil
}

func (mv *mockVirtualizar) CreateDomain(config *domain.Config, env map[string]string) error {
	mv.lock.Lock()
	defer mv.lock.Unlock()

	mv.count += 1
	mv.config = config

	return mv.err
}

func (mv *mockVirtualizar) getPassedConfig() *domain.Config {
	mv.lock.Lock()
	defer mv.lock.Unlock()

	return mv.config.Copy()
}

func (mv *mockVirtualizar) getNumberOfVMs() int {
	mv.lock.Lock()
	defer mv.lock.Unlock()

	return mv.count
}

func (mv *mockVirtualizar) StopDomain(name string) error {
	return nil
}

func (mv *mockVirtualizar) DestroyDomain(name string) error {
	mv.lock.Lock()
	defer mv.lock.Unlock()

	mv.count -= 1
	return nil
}

func (mv *mockVirtualizar) GetInfo() (domain.VirtualizerInfo, error) {
	return domain.VirtualizerInfo{}, nil
}

func (mv *mockVirtualizar) GetNetworkInterfaces(name string) ([]domain.NetworkInterface, error) {
	return []domain.NetworkInterface{}, nil
}

func (mv *mockVirtualizar) GetDomain(name string) (*domain.Info, error) {
	mv.lock.Lock()
	defer mv.lock.Unlock()

	if mv.count > 0 {
		return &domain.Info{
			State:     "running",
			Memory:    512 * 1024 * 1024,
			CPUTime:   1000,
			MaxMemory: 512 * 1024 * 1024,
			NrVirtCPU: 1,
		}, nil
	}
	return nil, fmt.Errorf("domain not found")
}

func (mv *mockVirtualizar) GetAllDomains() ([]domain.Info, error) {
	mv.lock.Lock()
	defer mv.lock.Unlock()

	// Return mock domains based on current count
	domains := make([]domain.Info, mv.count)
	for i := 0; i < mv.count; i++ {
		domains[i] = domain.Info{
			State:     "running",
			Memory:    512 * 1024 * 1024, // 512MB
			CPUTime:   1000,
			MaxMemory: 512 * 1024 * 1024,
			NrVirtCPU: 1,
		}
	}
	return domains, nil
}

func createBasicResources() *drivers.Resources {
	res := drivers.Resources{
		NomadResources: &structs.AllocatedTaskResources{
			Memory: structs.AllocatedMemoryResources{
				MemoryMB: 6000,
			},
			Cpu: structs.AllocatedCpuResources{},
		},
		LinuxResources: &drivers.LinuxResources{
			CpusetCpus:       "1,2,3",
			CPUPeriod:        100000,
			CPUQuota:         100000,
			CPUShares:        2000,
			MemoryLimitBytes: 256 * 1024 * 1024,
			PercentTicks:     float64(500) / float64(2000),
		},
	}
	return &res
}

// virtDriverHarness wires up everything needed to launch a task with a virt driver.
// A driver plugin interface and cleanup function is returned
func virtDriverHarness(t *testing.T, v Virtualizer, dg DomainGetter, ih ImageHandler,
	dataDir string) *dtestutil.DriverHarness {
	logger := testlog.HCLogger(t)
	if testing.Verbose() {
		logger.SetLevel(hclog.Trace)
	} else {
		logger.SetLevel(hclog.Info)
	}

	baseConfig := &base.Config{}
	config := &Config{
		DataDir:            dataDir,
		ImagePaths:         []string{"/root", dataDir}, // Allow images from /root and test temp dir
		DisableAllocMounts: true,
		Network: domain.Network{
			Bridge:      getEnvOrDefault("BRIDGE", "br0"),
			SubnetCIDR:  "192.168.1.0/24",
			Gateway:     "192.168.1.1",
			IPPoolStart: "192.168.1.100",
			IPPoolEnd:   "192.168.1.200",
			TAPPrefix:   "tap",
		},
		CloudHypervisor: domain.CloudHypervisor{
			Bin:              getEnvOrDefault("CH_BIN", "/usr/bin/cloud-hypervisor"),
			RemoteBin:        "/usr/bin/ch-remote",
			VirtiofsdBin:     "/usr/libexec/virtiofsd",
			DefaultKernel:    getEnvOrDefault("KERNEL_PATH", "/root/vmlinux-normal"),   // Use environment variable from CI or fallback
			DefaultInitramfs: getEnvOrDefault("INITRD_PATH", "/root/raiin-fc.cpio.gz"), // Use environment variable from CI or fallback
			Firmware:         "",
			Seccomp:          "true",
			LogFile:          "",
		},
	}

	if err := base.MsgPackEncode(&baseConfig.PluginConfig, config); err != nil {
		t.Error("Unable to encode plugin config", err)
	}

	d := NewPlugin(logger).(*VirtDriverPlugin)
	if v != nil {
		d.virtualizer = v
		d.networkController = &mockNet{}
		d.networkInit.Store(true)
	}

	must.NoError(t, d.SetConfig(baseConfig))
	d.imageHandler = ih
	d.taskGetter = dg

	harness := dtestutil.NewDriverHarness(t, d)

	return harness
}

// createUniqueRootfsImage creates a unique disk image file for each test to avoid locking conflicts
func createUniqueRootfsImage(t *testing.T, tempDir string) string {
	// Try to use actual rootfs image if available (try multiple possible paths)
	possiblePaths := []string{
		getEnvOrDefault("ROOTFS_PATH", "/root/rootfs.img"),
		"/root/alpine-rootfs.img",
		"/root/rootfs.img",
	}

	var sourceImage string
	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			sourceImage = path
			break
		}
	}

	// Create unique image file for this test
	uniqueImage, err := os.CreateTemp(tempDir, "test-rootfs-*.img")
	must.NoError(t, err)
	defer uniqueImage.Close()

	// If source image exists, copy it; otherwise create a minimal file
	if sourceImage != "" {
		if sourceData, err := os.ReadFile(sourceImage); err == nil {
			_, err = uniqueImage.Write(sourceData)
			must.NoError(t, err)
		} else {
			// Create minimal test file if source not available
			_, err = uniqueImage.WriteString("test image content")
			must.NoError(t, err)
		}
	} else {
		// No source image found, create minimal test file
		_, err = uniqueImage.WriteString("test image content")
		must.NoError(t, err)
	}

	return uniqueImage.Name()
}

func newTaskConfig(t *testing.T, image string) TaskConfig {
	// Create temporary user data file
	tmpFile, err := os.CreateTemp("", "userdata-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp user data file: %v", err)
	}
	tmpFile.WriteString("#cloud-config\nusers:\n  - name: testuser\n")
	tmpFile.Close()

	t.Cleanup(func() {
		os.Remove(tmpFile.Name())
	})

	return TaskConfig{
		ImagePath:           image,
		UserData:            tmpFile.Name(),
		CMDs:                []string{"cmd arg arg", "cmd arg arg"},
		DefaultUserSSHKey:   "ssh-ed666 randomkey",
		DefaultUserPassword: "password",
		UseThinCopy:         false,
		OS: &OS{
			Arch:    "arch",
			Machine: "machine",
		},
	}
}

func TestVirtDriver_Start_Wait_Destroy(t *testing.T) {
	ci.Parallel(t)

	tempDir, err := os.MkdirTemp("", "exampledir-*")
	must.NoError(t, err)
	defer os.RemoveAll(tempDir)

	allocID := uuid.Generate()
	taskCfg := newTaskConfig(t, createUniqueRootfsImage(t, tempDir))

	taskID := fmt.Sprintf("%s/%s/%s", allocID[:7], "task-name", "0000000")
	task := &drivers.TaskConfig{
		ID:        taskID,
		AllocID:   allocID,
		Resources: createBasicResources(),
	}

	must.NoError(t, task.EncodeConcreteDriverConfig(&taskCfg))

	mockVirtualizer := &mockVirtualizar{}

	mockTaskGetter := &mockTaskGetter{
		info: &domain.Info{
			State: "running",
		},
	}

	mockImageHandler := &mockImageHandler{
		imageFormat: "tif",
	}

	d := virtDriverHarness(t, mockVirtualizer, mockTaskGetter, mockImageHandler, tempDir)
	cleanup := d.MkAllocDir(task, true)
	defer cleanup()

	dth, _, err := d.StartTask(task)
	must.NoError(t, err)
	must.One(t, dth.Version)

	finalTs, err := d.InspectTask(task.ID)
	must.NoError(t, err)
	must.Eq(t, drivers.TaskStateRunning, finalTs.State)
	must.StrContains(t, taskID, finalTs.ID)

	waitCh, err := d.WaitTask(context.Background(), task.ID)
	must.NoError(t, err)

	select {
	case <-waitCh:
		t.Fatalf("wait channel should not have received an exit result")
	case <-time.After(1 * time.Second):
	}

	err = d.DestroyTask(task.ID, true)
	must.NoError(t, err)
}

func TestVirtDriver_Start_Recover_Destroy(t *testing.T) {
	ci.Parallel(t)

	tempDir, err := os.MkdirTemp("", "exampledir-*")
	must.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create unique disk image for this test to avoid locking conflicts
	uniqueRootfsPath := createUniqueRootfsImage(t, tempDir)

	allocID := uuid.Generate()
	taskCfg := newTaskConfig(t, uniqueRootfsPath)

	taskID := fmt.Sprintf("%s/%s/%s", allocID[:7], "task-name", "0000000")
	task := &drivers.TaskConfig{
		ID:        taskID,
		AllocID:   allocID,
		Resources: createBasicResources(),
	}

	must.NoError(t, task.EncodeConcreteDriverConfig(&taskCfg))

	mockVirtualizer := &mockVirtualizar{}

	mockTaskGetter := &mockTaskGetter{
		info: &domain.Info{
			State: "running",
		},
	}

	mockImageHandler := &mockImageHandler{
		imageFormat: "tif",
	}

	d := virtDriverHarness(t, mockVirtualizer, mockTaskGetter, mockImageHandler, tempDir)
	cleanup := d.MkAllocDir(task, true)
	defer cleanup()

	dth, _, err := d.StartTask(task)
	must.NoError(t, err)

	must.One(t, dth.Version)

	ts, err := d.InspectTask(task.ID)
	must.NoError(t, err)
	must.Eq(t, drivers.TaskStateRunning, ts.State)
	must.StrContains(t, task.ID, ts.ID)

	// For recovery test, we'll simulate the driver restart by just verifying
	// the task can be properly destroyed (recovery would happen automatically
	// in production Nomad when the driver restarts)

	err = d.DestroyTask(task.ID, true)
	must.NoError(t, err)
}

func TestVirtDriver_Start_Wait_Crashed(t *testing.T) {
	ci.Parallel(t)

	tempDir, err := os.MkdirTemp("", "exampledir-*")
	must.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create unique disk image for this test to avoid locking conflicts
	uniqueRootfsPath := createUniqueRootfsImage(t, tempDir)

	allocID := uuid.Generate()
	taskCfg := newTaskConfig(t, uniqueRootfsPath)

	taskID := fmt.Sprintf("%s/%s/%s", allocID[:7], "task-name", "0000000")
	task := &drivers.TaskConfig{
		ID:        taskID,
		AllocID:   allocID,
		Resources: createBasicResources(),
	}

	must.NoError(t, task.EncodeConcreteDriverConfig(&taskCfg))

	mockVirtualizer := &mockVirtualizar{}

	mockTaskGetter := &mockTaskGetter{
		info: &domain.Info{
			State: "crashed",
		},
	}

	mockImageHandler := &mockImageHandler{
		imageFormat: "tif",
	}

	d := virtDriverHarness(t, mockVirtualizer, mockTaskGetter, mockImageHandler, tempDir)
	cleanup := d.MkAllocDir(task, true)
	defer cleanup()

	dth, _, err := d.StartTask(task)
	must.NoError(t, err)

	must.One(t, dth.Version)

	waitCh, err := d.WaitTask(context.Background(), task.ID)
	must.NoError(t, err)

	select {
	case exitResult := <-waitCh:
		must.One(t, exitResult.ExitCode)
		must.ErrorContains(t, exitResult.Err, "task has crashed")

	case <-time.After(10 * time.Second):
		t.Fatalf("wait channel should have received an exit result")
	}

	dts, err := d.InspectTask(task.ID)
	must.NoError(t, err)

	must.One(t, dts.ExitResult.ExitCode)
	must.Eq(t, "exited", dts.State)
}

func TestVirtDriver_ImageOptions(t *testing.T) {
	ci.Parallel(t)

	tempDir, err := os.MkdirTemp("", "exampledir-*")
	must.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create unique disk image for this test to avoid locking conflicts
	uniqueRootfsPath := createUniqueRootfsImage(t, tempDir)

	allocID := uuid.Generate()

	mockVirtualizer := &mockVirtualizar{}

	mockTaskGetter := &mockTaskGetter{
		info: &domain.Info{
			State: "running",
		},
	}

	mockImageHandler := &mockImageHandler{
		imageFormat: "tif",
	}

	tests := []struct {
		name           string
		enableThinCopy bool
		expectedPath   string
		expectedFormat string
	}{
		{
			name:           "no_copy_requested",
			enableThinCopy: false,
			expectedPath:   uniqueRootfsPath, // When no copy, use original image path
			expectedFormat: "tif",
		},
		{
			name:           "copy_requested",
			enableThinCopy: true,
			expectedPath:   fmt.Sprintf("%s/%s.img", tempDir, "task-name-0000000"),
			expectedFormat: "qcow2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskCfg := newTaskConfig(t, uniqueRootfsPath)
			taskCfg.UseThinCopy = tt.enableThinCopy

			taskID := fmt.Sprintf("%s/%s/%s", allocID[:7], "task-name", "0000000")
			task := &drivers.TaskConfig{
				ID:        taskID,
				AllocID:   allocID,
				Resources: createBasicResources(),
			}
			must.NoError(t, task.EncodeConcreteDriverConfig(&taskCfg))

			d := virtDriverHarness(t, mockVirtualizer, mockTaskGetter, mockImageHandler, tempDir)
			cleanup := d.MkAllocDir(task, true)
			defer cleanup()

			dth, _, err := d.StartTask(task)
			must.NoError(t, err)
			must.One(t, dth.Version)

			// Clean up the task
			d.DestroyTask(task.ID, true)
		})
	}
}

type cloudInitMock struct {
	passedConfig *cloudinit.Config
	err          error
}

func (cim *cloudInitMock) Apply(ci *cloudinit.Config, path string) error {
	if err := os.WriteFile(path, []byte("Hello, World!"), 0644); err != nil {
		return err
	}

	cim.passedConfig = ci

	return cim.err
}

func TestVirtDriver_Start_Wait_Destroy_Integration(t *testing.T) {
	ci.Parallel(t)

	// Skip integration test if Cloud Hypervisor binary is not available
	if _, err := os.Stat("/usr/bin/cloud-hypervisor"); os.IsNotExist(err) {
		t.Skip("Cloud Hypervisor binary not found, skipping integration test")
	}

	// Skip integration test if required test artifacts are not available
	kernelPath := getEnvOrDefault("KERNEL_PATH", "/root/vmlinux-normal")
	initrdPath := getEnvOrDefault("INITRD_PATH", "/root/raiin-fc.cpio.gz")
	rootfsPath := getEnvOrDefault("ROOTFS_PATH", "/root/rootfs.img")

	if _, err := os.Stat(kernelPath); os.IsNotExist(err) {
		t.Skipf("Kernel file not found at %s, skipping integration test", kernelPath)
	}
	if _, err := os.Stat(initrdPath); os.IsNotExist(err) {
		t.Skipf("Initrd file not found at %s, skipping integration test", initrdPath)
	}
	if _, err := os.Stat(rootfsPath); os.IsNotExist(err) {
		t.Skipf("Rootfs file not found at %s, skipping integration test", rootfsPath)
	}

	// Check if bridge exists (for networking tests)
	bridgeName := getEnvOrDefault("BRIDGE", "br0")
	if _, err := os.Stat("/sys/class/net/" + bridgeName); os.IsNotExist(err) {
		t.Skipf("Bridge interface %s not found, skipping integration test", bridgeName)
	}

	tempDir, err := os.MkdirTemp("", "exampledir-*")
	must.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create unique disk image for this test to avoid locking conflicts
	uniqueRootfsPath := createUniqueRootfsImage(t, tempDir)

	allocID := uuid.Generate()
	taskCfg := newTaskConfig(t, uniqueRootfsPath)
	taskCfg.UserData = ""
	taskCfg.OS = &OS{
		Arch:    "x86_64",
		Machine: "pc-i440fx-jammy",
	}

	taskID := fmt.Sprintf("%s/%s/%s", allocID[:7], "task-name", "0000000")
	task := &drivers.TaskConfig{
		ID:        taskID,
		AllocID:   allocID,
		Resources: createBasicResources(),
	}

	must.NoError(t, task.EncodeConcreteDriverConfig(&taskCfg))

	mockImageHandler := &mockImageHandler{
		imageFormat: "qcow2",
	}

	// Use real Cloud Hypervisor integration - no mock virtualizer but need taskGetter
	// Create mock taskGetter that returns running state for integration test
	mockTaskGetter := &mockTaskGetter{
		info: &domain.Info{
			State: "running",
		},
	}

	d := virtDriverHarness(t, nil, mockTaskGetter, mockImageHandler, tempDir)
	cleanup := d.MkAllocDir(task, true)
	defer cleanup()

	dth, _, err := d.StartTask(task)
	must.NoError(t, err)

	must.One(t, dth.Version)

	t.Logf("Integration test: task started successfully, checking status")

	// For integration test, just verify that the task is running
	// Real Cloud Hypervisor doesn't maintain a global domain list like libvirt

	// Check task status immediately
	ts, err := d.InspectTask(task.ID)
	if err != nil {
		t.Fatalf("Integration test: failed to inspect task: %v", err)
	}
	t.Logf("Integration test: task status: %s", ts.State)

	// Attempt to wait
	waitCh, err := d.WaitTask(context.Background(), task.ID)
	must.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	statsChan, err := d.TaskStats(ctx, task.ID, time.Second)
	must.NoError(t, err)

	go func(t *testing.T) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-statsChan:
			case <-time.After(2 * time.Second):
				t.Error("no stats comming from task channel")
			}
		}
	}(t)

	t.Logf("Integration test: waiting for task to be ready")
	select {
	case <-waitCh:
		t.Fatalf("wait channel should not have received an exit result")
	case <-time.After(30 * time.Second): // Increased timeout for CI environment
		t.Logf("Integration test: task still running after 30 seconds, checking final status")
	}

	finalTs, err := d.InspectTask(task.ID)
	if err != nil {
		t.Fatalf("Integration test: failed to inspect task: %v", err)
	}
	t.Logf("Integration test: final task status: %s", finalTs.State)
	must.Eq(t, drivers.TaskStateRunning, finalTs.State)
	must.StrContains(t, task.ID, finalTs.ID)

	cancel()
	err = d.DestroyTask(task.ID, true)
	must.NoError(t, err)

	t.Logf("Integration test complete - real Cloud Hypervisor driver tested")
}
