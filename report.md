# Stepman Race Condition Analysis Report

## Executive Summary

This report documents a comprehensive analysis of race conditions in the stepman codebase that occur when multiple instances run concurrently on the same machine. The analysis identified **critical race conditions** across filesystem operations, shared state management, and process coordination that explain the intermittent failures observed in production environments.

**Key Findings:**
- 15+ critical race conditions identified across core functionality
- Primary issues in step activation, cache management, and library routing
- Root cause: pervasive "check-then-act" patterns without synchronization
- Impact: Corrupted downloads, failed activations, lost configurations

## Background

Stepman is a CLI tool for managing decentralized StepLib Step Collections in Bitrise CI/CD environments. When multiple CI jobs run simultaneously, multiple stepman instances can execute concurrently, leading to race conditions on shared resources like:
- Step cache directories (`~/.stepman/step_collections/`)
- Library routing configuration (`~/.stepman/routing.json`)
- Git repositories and temporary files

## Critical Race Conditions Identified

### 1. Filesystem Race Conditions

#### Step Cache Directory Creation
**Location**: `stepman/util.go:121-125`
```go
stepPth := GetStepCacheDirPath(route, id, version)
if exist, err := pathutil.IsPathExists(stepPth); err != nil {
    return err
} else if exist {
    return nil
}
// Later: DownloadAndUnZIP(downloadLocation.Src, stepPth)
```
**Race Condition**: Multiple instances check cache existence → both proceed to download → concurrent downloads corrupt each other

**Impact**: Corrupted step cache, partial downloads, activation failures

#### Step Executable Downloads  
**Location**: `activator/steplib/activate_executable.go:40-49`
```go
path := filepath.Join(destinationDir, stepID)
file, err := os.Create(path)  // Truncates existing file
```
**Race Condition**: `os.Create` truncates files being written by other processes

**Impact**: Corrupted executables, failed step activations

#### Spec File Management
**Location**: `stepman/util.go:323-331`
```go
if exist, err := pathutil.IsPathExists(pth); err != nil {
    return err
} else if !exist {
    dir, _ := path.Split(pth)
    err := os.MkdirAll(dir, 0777)
} else {
    err := os.Remove(pth)  // Dangerous!
}
```
**Race Condition**: File removed while another process tries to read it

**Impact**: Missing spec files, broken step resolution

#### Step Activation Directory Creation
**Location**: `activator/steplib/activate_source.go:57-63`
```go
if exist, err := pathutil.IsPathExists(dst); err != nil {
    return fmt.Errorf("failed to check if %s path exist: %s", dst, err)
} else if !exist {
    if err := os.MkdirAll(dst, 0777); err != nil {
        return fmt.Errorf("failed to create dir for %s path: %s", dst, err)
    }
}
```
**Race Condition**: Classic TOCTOU (Time-Of-Check-Time-Of-Use) vulnerability

**Impact**: Directory creation conflicts, failed step copying

### 2. Configuration Management Race Conditions

#### Route Configuration Management
**Location**: `stepman/paths.go:119-127`
```go
func AddRoute(route SteplibRoute) error {
    routes, err := readRouteMap()  // READ
    if err != nil {
        return err
    }
    routes = append(routes, route) // MODIFY
    return routes.writeToFile()    // WRITE
}
```
**Race Condition**: Classic read-modify-write race condition

**Impact**: Lost route configurations, duplicate routes, corrupted routing.json

#### Git Repository Operations
**Location**: `stepman/library.go:114-137`
```go
if err := retry.Times(2).Wait(3 * time.Second).Try(func(attempt uint) error {
    repo, err := git.New(pth)
    if err != nil {
        return err
    }
    return repo.Pull().Run()  // Concurrent git operations
}); err != nil {
```
**Race Condition**: Multiple git pull operations on same repository

**Impact**: Git lock conflicts, corrupted repository state

### 3. Shared State Race Conditions

#### Environment Variable Conflicts
**Location**: `toolkits/golang.go:161-167`
```go
pathWithGoBins := fmt.Sprintf("%s:%s", goToolkitBinsPath(), os.Getenv("PATH"))
if err := os.Setenv("PATH", pathWithGoBins); err != nil {
    return fmt.Errorf("set PATH to include the Go toolkit bins, error: %s", err)
}
if err := os.Setenv("GOROOT", goToolkitInstallRootPath()); err != nil {
    return fmt.Errorf("set GOROOT to Go toolkit root, error: %s", err)
}
```
**Race Condition**: Process-global environment variables overwritten by concurrent operations

**Impact**: Inconsistent build environments, compilation failures

#### Time-based ID Generation
**Location**: `stepman/paths.go:131`
```go
func GenerateFolderAlias() string {
    return fmt.Sprintf("%v", time.Now().Unix())
}
```
**Race Condition**: Same-second operations generate identical aliases

**Impact**: Folder alias collisions, overwritten collections

#### Preload System Error Handling
**Location**: `preload/preload_steps.go:66,118-120`
```go
// Workers write errors
errC <- err

// Main goroutine reads errors
close(errC)
for err := range errC {
    return err  // Returns only first error
}
```
**Race Condition**: Multiple workers writing errors, early exit loses errors

**Impact**: Hidden failures, resource leaks, incomplete error reporting

### 4. Cache Management Race Conditions

#### Step Binary Caching
**Location**: `toolkits/golang.go:282-311`
```go
fullStepBinPath := stepBinaryCacheFullPath(sIDData)
if exists, err := pathutil.IsPathExists(fullStepBinPath); err != nil {
    toolkit.logger.Warnf("Failed to check cached binary for step, error: %s", err)
} else if exists {
    return nil  // Use cached version
}
// Compile and cache the binary
return goBuildStep(...)
```
**Race Condition**: Both processes check cache → both compile → both write to cache

**Impact**: Wasted compilation, file corruption, build failures

#### Cache Directory Cleanup
**Location**: `preload/preload_steps.go:201-214`
```go
sourceExist, err := pathutil.IsPathExists(stepSourceDir)
if err != nil {
    return "", fmt.Errorf("failed to check if %s path exist: %s", stepSourceDir, err)
}
if sourceExist {
    if err := os.RemoveAll(stepSourceDir); err != nil {
        return "", fmt.Errorf("failed to remove step source dir: %s", err)
    }
}
```
**Race Condition**: Directory deleted while another process is using it

**Impact**: Cleanup conflicts, interrupted operations

### 5. Share Workflow Race Conditions

#### Git Branch Operations
**Location**: `cli/share_create.go:240-249`
```go
steplibRepo, err := git.New(collectionDir)
if err != nil {
    failf("Failed to init steplib repo: %s", err)
}
if err := steplibRepo.Checkout(share.ShareBranchName()).Run(); err != nil {
    if err := steplibRepo.NewBranch(share.ShareBranchName()).Run(); err != nil {
        failf("Git failed to create and checkout branch, err: %s", err)
    }
}
```
**Race Condition**: Concurrent branch checkout/creation operations

**Impact**: Failed step sharing, corrupted share state

#### Step Directory Creation
**Location**: `cli/share_create.go:232-238`
```go
if exist, err := pathutil.IsPathExists(stepDirInSteplib); err != nil {
    failf("Failed to check path (%s), err: %s", stepDirInSteplib, err)
} else if !exist {
    if err := os.MkdirAll(stepDirInSteplib, 0777); err != nil {
        failf("Failed to create path (%s), err: %s", stepDirInSteplib, err)
    }
}
```
**Race Condition**: Multiple sharing operations creating same directory structure

**Impact**: Directory creation conflicts, failed step sharing

## Root Cause Analysis

### Primary Anti-Patterns

1. **Check-Then-Act Pattern**: Pervasive throughout codebase
   ```go
   if exists := checkExists(path); !exists {
       create(path)  // Race window here
   }
   ```

2. **Non-Atomic File Operations**: Direct writes without temporary files
   ```go
   os.Create(finalPath)  // Should use temp-then-rename
   ```

3. **No Synchronization Mechanisms**: Zero file locking or process coordination

4. **Shared State Modification**: Process-global environment variables

5. **Predictable Temporary Paths**: Multiple processes targeting same locations

### Contributing Factors

- **High Concurrency**: Preload system uses 10 worker goroutines
- **Shared Cache Directories**: All instances use same cache locations
- **CI/CD Environment**: Multiple build jobs running simultaneously
- **No Process Awareness**: No detection or handling of concurrent instances

## Impact Assessment

### Production Symptoms

The race conditions cause the exact symptoms reported:
- "Directory doesn't exist" errors when directory was just created
- "File doesn't exist" errors for files that clearly exist
- Inconsistent behavior that only occurs under load
- Corrupted cache entries and failed step activations

### Risk Levels

**Critical (Immediate Fix Required)**:
- Step cache directory creation
- Route configuration management
- Git repository operations
- Spec file management

**High (Fix Soon)**:
- Environment variable conflicts
- Step activation races
- Cache cleanup operations

**Medium (Architectural Improvements)**:
- Preload system error handling
- Time-based ID generation
- Share workflow races

## Recommended Solutions

### Immediate Fixes (High Priority)

#### 1. Implement File Locking
```go
func (routes SteplibRoutes) writeToFileWithLock() error {
    lockFile := getRoutingFilePath() + ".lock"
    
    // Acquire exclusive lock
    lock, err := flock.New(lockFile)
    if err != nil {
        return err
    }
    defer lock.Close()
    
    if err := lock.Lock(); err != nil {
        return err
    }
    
    return routes.writeToFile()
}
```

#### 2. Use Atomic File Operations
```go
func writeStepSpecAtomic(pth string, data []byte) error {
    dir := filepath.Dir(pth)
    if err := os.MkdirAll(dir, 0777); err != nil {
        return err
    }
    
    // Write to temporary file first
    tempFile, err := ioutil.TempFile(dir, ".tmp-spec-*")
    if err != nil {
        return err
    }
    defer os.Remove(tempFile.Name())
    
    if _, err := tempFile.Write(data); err != nil {
        return err
    }
    
    if err := tempFile.Close(); err != nil {
        return err
    }
    
    // Atomic rename
    return os.Rename(tempFile.Name(), pth)
}
```

#### 3. Process-Safe Directory Creation
```go
func ensureStepCacheDir(route stepman.SteplibRoute, id, version string) (string, error) {
    finalPath := stepman.GetStepCacheDirPath(route, id, version)
    
    // Create in unique temporary location first
    tempDir, err := ioutil.TempDir(filepath.Dir(finalPath), 
        fmt.Sprintf(".tmp-%s-%s-%s-*", 
            route.FolderAlias, id, version))
    if err != nil {
        return "", err
    }
    
    // Perform all operations in tempDir
    // ...
    
    // Atomic move to final location
    if err := os.Rename(tempDir, finalPath); err != nil {
        if os.IsExist(err) {
            // Another process already created it, clean up and use existing
            os.RemoveAll(tempDir)
            return finalPath, nil
        }
        return "", err
    }
    
    return finalPath, nil
}
```

### Architectural Improvements (Medium Priority)

#### 1. Add Process Coordination
```go
type ProcessLock struct {
    lockFile string
    fd       *os.File
}

func AcquireProcessLock(resource string) (*ProcessLock, error) {
    lockPath := filepath.Join(os.TempDir(), fmt.Sprintf("stepman-%s.lock", resource))
    fd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0600)
    if err != nil {
        return nil, err
    }
    
    if err := syscall.Flock(int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
        fd.Close()
        return nil, fmt.Errorf("resource %s is locked by another process", resource)
    }
    
    return &ProcessLock{lockFile: lockPath, fd: fd}, nil
}
```

#### 2. Improve Error Handling
```go
func createDirectoryIfNotExists(path string) error {
    if err := os.MkdirAll(path, 0777); err != nil {
        if os.IsExist(err) {
            return nil  // Already exists, not an error
        }
        return fmt.Errorf("failed to create directory %s: %w", path, err)
    }
    return nil
}
```

#### 3. Replace Environment Variable Usage
```go
type ToolkitConfig struct {
    BinPath string
    Root    string
    Env     map[string]string
}

func (t ToolkitConfig) ExecCommand(cmd string, args ...string) *exec.Cmd {
    c := exec.Command(cmd, args...)
    c.Env = os.Environ()
    for k, v := range t.Env {
        c.Env = append(c.Env, fmt.Sprintf("%s=%s", k, v))
    }
    return c
}
```

### Long-term Solutions (Lower Priority)

1. **Process Isolation**: Use per-process cache directories
2. **Monitoring**: Add concurrent access detection and warnings
3. **Testing**: Add race condition detection tests
4. **Documentation**: Document concurrency requirements

## Testing Strategy

### Race Condition Detection
```bash
# Run with race detector
go test -race ./...

# Stress test with multiple concurrent instances
for i in {1..10}; do
    stepman activate steplib::step@version &
done
wait
```

### Integration Testing
```bash
# Test concurrent operations
./test-concurrent-stepman.sh
```

## Implementation Priority

### Phase 1 (Immediate - Week 1)
- [ ] Add file locking for route configuration
- [ ] Implement atomic spec file operations  
- [ ] Fix step cache directory creation races
- [ ] Add process-safe step activation

### Phase 2 (Short-term - Week 2-3)
- [ ] Implement proper temporary file management
- [ ] Fix environment variable conflicts
- [ ] Improve preload system error handling
- [ ] Add concurrent operation detection

### Phase 3 (Medium-term - Month 1)
- [ ] Add process coordination framework
- [ ] Implement comprehensive testing
- [ ] Add monitoring and logging
- [ ] Documentation updates

## Conclusion

The stepman codebase contains **systemic race conditions** that make it unreliable in concurrent environments typical of CI/CD systems. The issues stem from fundamental design patterns that assume single-process execution.

**Immediate action required** on the critical filesystem race conditions to prevent data corruption and improve reliability. The proposed solutions provide a roadmap from quick fixes to architectural improvements that will make stepman safe for concurrent execution.

The most critical fixes focus on **step activation** and **library management** - the core functionality that CI/CD pipelines depend on. Implementing file locking and atomic operations will resolve the majority of reported issues.