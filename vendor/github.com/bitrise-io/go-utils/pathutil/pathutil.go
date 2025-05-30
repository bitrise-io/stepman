package pathutil

import (
	"errors"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

// NormalizedOSTempDirPath ...
// Creates a temp dir, and returns its path.
// If tmpDirNamePrefix is provided it'll be used
//  as the tmp dir's name prefix.
// Normalized: it's guaranteed that the path won't end with '/'.
func NormalizedOSTempDirPath(tmpDirNamePrefix string) (retPth string, err error) {
	retPth, err = ioutil.TempDir("", tmpDirNamePrefix)
	if strings.HasSuffix(retPth, "/") {
		retPth = retPth[:len(retPth)-1]
	}
	return
}

// CurrentWorkingDirectoryAbsolutePath ...
func CurrentWorkingDirectoryAbsolutePath() (string, error) {
	return filepath.Abs("./")
}

// UserHomeDir ...
func UserHomeDir() string {
	if runtime.GOOS == "windows" {
		home := os.Getenv("HOMEDRIVE") + os.Getenv("HOMEPATH")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		return home
	}
	return os.Getenv("HOME")
}

// EnsureDirExist ...
func EnsureDirExist(dir string) error {
	exist, err := IsDirExists(dir)
	if !exist || err != nil {
		return os.MkdirAll(dir, 0755)
	}
	return nil
}

func genericIsPathExists(pth string) (os.FileInfo, bool, error) {
	if pth == "" {
		return nil, false, errors.New("no path provided")
	}
	fileInf, err := os.Lstat(pth)
	if err == nil {
		return fileInf, true, nil
	}
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	return fileInf, false, err
}

// PathCheckAndInfos ...
// Returns:
// 1. file info or nil
// 2. bool, indicating whether the path exists
// 3. error, if any error happens during the check
func PathCheckAndInfos(pth string) (os.FileInfo, bool, error) {
	return genericIsPathExists(pth)
}

// IsDirExists ...
func IsDirExists(pth string) (bool, error) {
	fileInf, isExists, err := genericIsPathExists(pth)
	if err != nil {
		return false, err
	}
	if !isExists {
		return false, nil
	}
	if fileInf == nil {
		return false, errors.New("no file info available")
	}
	return fileInf.IsDir(), nil
}

// IsPathExists ...
func IsPathExists(pth string) (bool, error) {
	_, isExists, err := genericIsPathExists(pth)
	return isExists, err
}

//
// Path modifier functions

// PathModifier ...
type PathModifier interface {
	AbsPath(pth string) (string, error)
}

type defaultPathModifier struct{}

// NewPathModifier ...
func NewPathModifier() PathModifier {
	return defaultPathModifier{}
}

// AbsPath ...
func (defaultPathModifier) AbsPath(pth string) (string, error) {
	return AbsPath(pth)
}

// AbsPath expands ENV vars and the ~ character
//	then call Go's Abs
func AbsPath(pth string) (string, error) {
	if pth == "" {
		return "", errors.New("no Path provided")
	}

	pth, err := ExpandTilde(pth)
	if err != nil {
		return "", err
	}

	return filepath.Abs(os.ExpandEnv(pth))
}

// ExpandTilde ...
func ExpandTilde(pth string) (string, error) {
	if pth == "" {
		return "", errors.New("no Path provided")
	}

	if strings.HasPrefix(pth, "~") {
		pth = strings.TrimPrefix(pth, "~")

		if len(pth) == 0 || strings.HasPrefix(pth, "/") {
			return os.ExpandEnv("$HOME" + pth), nil
		}

		splitPth := strings.Split(pth, "/")
		username := splitPth[0]

		usr, err := user.Lookup(username)
		if err != nil {
			return "", err
		}

		pathInUsrHome := strings.Join(splitPth[1:], "/")

		return filepath.Join(usr.HomeDir, pathInUsrHome), nil
	}

	return pth, nil
}

// IsRelativePath ...
func IsRelativePath(pth string) bool {
	if strings.HasPrefix(pth, "./") {
		return true
	}

	if strings.HasPrefix(pth, "/") {
		return false
	}

	if strings.HasPrefix(pth, "$") {
		return false
	}

	return true
}

// GetFileName returns the name of the file from a given path or the name of the directory if it is a directory
func GetFileName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

// EscapeGlobPath escapes a partial path, determined at runtime, used as a parameter for filepath.Glob
func EscapeGlobPath(path string) string {
	var escaped string
	for _, ch := range path {
		if ch == '[' || ch == ']' || ch == '-' || ch == '*' || ch == '?' || ch == '\\' {
			escaped += "\\"
		}
		escaped += string(ch)
	}
	return escaped
}

//
// Change dir functions

// RevokableChangeDir ...
func RevokableChangeDir(dir string) (func() error, error) {
	origDir, err := CurrentWorkingDirectoryAbsolutePath()
	if err != nil {
		return nil, err
	}

	revokeFn := func() error {
		return os.Chdir(origDir)
	}

	return revokeFn, os.Chdir(dir)
}

// ChangeDirForFunction ...
func ChangeDirForFunction(dir string, fn func()) error {
	revokeFn, err := RevokableChangeDir(dir)
	if err != nil {
		return err
	}

	fn()

	return revokeFn()
}
