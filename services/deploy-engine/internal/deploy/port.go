package deploy

import (
	"fmt"
	"net"
)

// freeHostPort asks the kernel for an unused TCP port by binding :0 and
// immediately releasing it. The small race window between release and
// `docker run -p` is acceptable for local/dev preview deployments — this is
// deliberately not a production port allocator.
func freeHostPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate host port: %w", err)
	}
	defer l.Close()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("allocate host port: unexpected addr type %T", l.Addr())
	}
	return addr.Port, nil
}
