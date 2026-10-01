package disk

import (
	"context"
	"io/fs"
	"strconv"
	"strings"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

func isLink(info fs.FileInfo) bool { return info.Mode()&fs.ModeSymlink != 0 }

func Volumes(ctx context.Context) ([]Volume, error) {
	out, err := (sysx.ExecRunner{}).Run(ctx, sysx.C("df", "-kP"))
	if err != nil {
		return nil, err
	}
	return decodeVolumes(out), nil
}

func decodeVolumes(out string) []Volume {
	var volumes []Volume
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || !strings.HasPrefix(fields[0], "/dev/") {
			continue
		}
		total, e := strconv.ParseInt(fields[1], 10, 64)
		if e != nil {
			continue
		}
		free, e := strconv.ParseInt(fields[3], 10, 64)
		if e != nil {
			continue
		}
		volumes = append(volumes, Volume{Name: fields[0], Path: strings.Join(fields[5:], " "), Total: total * 1024, Free: free * 1024})
	}
	return volumes
}
