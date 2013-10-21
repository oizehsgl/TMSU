/*
Copyright 2011-2013 Paul Ruane.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
	"tmsu/log"
	"tmsu/storage/database"
	"tmsu/vfs"
)

var MountCommand = Command{
	Name:     "mount",
	Synopsis: "Mount the virtual filesystem",
	Description: `tmsu mount
tmsu mount [OPTION]... [FILE] MOUNTPOINT

Without arguments, lists the currently mounted file-systems, otherwise mounts a
virtual file-system at the path MOUNTPOINT.

Where FILE is specified, the database at FILE is mounted.

If FILE is not specified but the TMSU_DB environment variable is defined then
the database at TMSU_DB is mounted.

Where neither FILE is specified nor TMSU_DB defined then the default database
is mounted.`,
	Options: Options{Option{"--allow-other", "-o", "allow other users access to the VFS (requires root or setting in fuse.conf)", false, ""}},
	Exec:    mountExec,
}

func mountExec(options Options, args []string) error {
	allowOther := options.HasOption("--allow-other")

	argCount := len(args)

	switch argCount {
	case 0:
		err := listMounts()
		if err != nil {
			return fmt.Errorf("could not list mounts: %v", err)
		}
	case 1:
		mountPath := args[0]

		err := mountDefault(mountPath, allowOther)
		if err != nil {
			return fmt.Errorf("could not mount database at '%v': %v", mountPath, err)
		}
	case 2:
		databasePath := args[0]
		mountPath := args[1]

		err := mountExplicit(databasePath, mountPath, allowOther)
		if err != nil {
			return fmt.Errorf("could not mount database '%v' at '%v': %v", databasePath, mountPath, err)
		}
	default:
		return fmt.Errorf("Too many arguments.")
	}

	return nil
}

func listMounts() error {
	log.Supp("retrieving mount table.")

	mt, err := vfs.GetMountTable()
	if err != nil {
		return fmt.Errorf("could not get mount table: %v", err)
	}

	if len(mt) == 0 {
		log.Supp("mount table is empty.")
	}

	for _, mount := range mt {
		log.Printf("'%v' at '%v'", mount.DatabasePath, mount.MountPath)
	}

	return nil
}

func mountDefault(mountPath string, allowOther bool) error {
	if err := mountExplicit(database.Path, mountPath, allowOther); err != nil {
		return err
	}

	return nil
}

func mountExplicit(databasePath string, mountPath string, allowOther bool) error {
	stat, err := os.Stat(mountPath)
	if err != nil {
		return fmt.Errorf("%v: could not stat: %v", mountPath, err)
	}
	if stat == nil {
		return fmt.Errorf("%v: mount point does not exist.", mountPath)
	}
	if !stat.IsDir() {
		return fmt.Errorf("%v: mount point is not a directory.", mountPath)
	}

	stat, err = os.Stat(databasePath)
	if err != nil {
		return fmt.Errorf("%v: could not stat: %v", databasePath, err)
	}
	if stat == nil {
		return fmt.Errorf("%v: database does not exist.")
	}

	log.Suppf("spawning daemon to mount VFS for database '%v' at '%v'.", databasePath, mountPath)

	args := []string{"vfs", databasePath, mountPath}
	if allowOther {
		args = append(args, "--allow-other")
	}

	daemon := exec.Command(os.Args[0], args...)

	errorPipe, err := daemon.StderrPipe()
	if err != nil {
		return fmt.Errorf("could not open standard error pipe: %v", err)
	}

	err = daemon.Start()
	if err != nil {
		return fmt.Errorf("could not start daemon: %v", err)
	}

	log.Supp("sleeping.")

	const HALF_SECOND = 500000000
	time.Sleep(HALF_SECOND)

	log.Supp("checking whether daemon started successfully.")

	var waitStatus syscall.WaitStatus
	var rusage syscall.Rusage
	_, err = syscall.Wait4(daemon.Process.Pid, &waitStatus, syscall.WNOHANG, &rusage)
	if err != nil {
		return fmt.Errorf("could not check daemon status: %v", err)
	}

	if waitStatus.Exited() {
		if waitStatus.ExitStatus() != 0 {
			buffer := make([]byte, 1024)
			count, err := errorPipe.Read(buffer)
			if err != nil {
				return fmt.Errorf("could not read from error pipe: %v", err)
			}

			return fmt.Errorf("virtual filesystem mount failed: %v", string(buffer[0:count]))
		}
	}

	return nil
}
