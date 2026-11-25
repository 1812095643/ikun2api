package adminapi

import (
	"errors"
	"net"
	"os"
	"path/filepath"
)

func Listen(path string) (net.Listener, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("absolute socket path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	if st, err := os.Lstat(path); err == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("refusing to replace non-socket path")
		}
		conn, e := net.Dial("unix", path)
		if e == nil {
			conn.Close()
			return nil, errors.New("socket already serving")
		}
		if e = os.Remove(path); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0660); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}
