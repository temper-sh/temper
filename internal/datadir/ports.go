package datadir

import (
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"
)

// A root's router gives its engines ports counting up from the root's start
// port. Each root has its own block of ports, so the engines of two roots
// never share a port, also while one root's router is idle and holds none.
const (
	// PortBlock is the number of engine ports in one root's block, and so the
	// most models one router configuration can hold.
	PortBlock = 32
	// DefaultStartPort is the default root's start port, which every root
	// used before Temper 0.3.0.
	DefaultStartPort = 10001
	// Blocks stay below macOS's ephemeral port range, 49152–65535.
	portBlocks = (49152 - DefaultStartPort) / PortBlock
)

// StartPort is the first engine port of root, a clean absolute path. The
// default root, ~/.temper, keeps DefaultStartPort, so its rendered
// configuration and generation do not change; every other root gets a block
// from the SHA-256 of its path, never the default block. It reads only $HOME.
func StartPort(root string) int {
	home, _ := os.UserHomeDir()
	return startPort(root, home)
}

func startPort(root, home string) int {
	if home != "" && root == filepath.Join(home, ".temper") {
		return DefaultStartPort
	}
	sum := sha256.Sum256([]byte(root))
	block := 1 + binary.BigEndian.Uint64(sum[:8])%(portBlocks-1)
	return DefaultStartPort + int(block)*PortBlock
}

// ValidStartPort reports whether port starts a block.
func ValidStartPort(port int) bool {
	offset := port - DefaultStartPort
	return offset >= 0 && offset%PortBlock == 0 && offset/PortBlock < portBlocks
}
