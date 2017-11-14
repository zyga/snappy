// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package main_test

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"
)

type utilsSuite struct {
	testutil.BaseTest
	sys *update.SyscallRecorder
	log *bytes.Buffer
}

var _ = Suite(&utilsSuite{})

func (s *utilsSuite) SetUpSuite(c *C) {
	logger.SimpleSetup()
}

func (s *utilsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.sys = &update.SyscallRecorder{}
	s.BaseTest.AddCleanup(update.MockSystemCalls(s.sys))
	buf, restore := logger.MockLogger()
	s.BaseTest.AddCleanup(restore)
	s.log = buf
}

func (s *utilsSuite) TearDownTest(c *C) {
	s.sys.CheckForStrayDescriptors(c)
	s.BaseTest.TearDownTest(c)
}

// Ensure that we refuse to create a directory with an relative path.
func (s *utilsSuite) TestSecureMkdirAllRelative(c *C) {
	err := update.SecureMkdirAll("rel/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot create directory with relative path: "rel/path"`)
	c.Assert(s.sys.Calls(), HasLen, 0)
}

// Ensure that we can "create the root directory.
func (s *utilsSuite) TestSecureMkdirAllLevel0(c *C) {
	c.Assert(update.SecureMkdirAll("/", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`close 3`,
	})
}

// Ensure that we can create a directory in the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllLevel1(c *C) {
	os.Setenv("SNAPD_DEBUG", "1")
	defer os.Unsetenv("SNAPD_DEBUG")
	c.Assert(update.SecureMkdirAll("/path", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "path" 0755`,
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`fchown 4 123 456`,
		`close 4`,
		`close 3`,
	})
	c.Assert(s.log.String(), testutil.Contains, `secure-mk-dir 3 ["path"] 0 -rwxr-xr-x 123 456 -> ...`)
	c.Assert(s.log.String(), testutil.Contains, `secure-mk-dir 3 ["path"] 0 -rwxr-xr-x 123 456 -> 4`)
}

// Ensure that we can create a directory two levels from the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllLevel2(c *C) {
	c.Assert(update.SecureMkdirAll("/path/to", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "path" 0755`,
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`fchown 4 123 456`,
		`close 3`,
		`mkdirat 4 "to" 0755`,
		`openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`fchown 3 123 456`,
		`close 3`,
		`close 4`,
	})
}

// Ensure that we can create a directory three levels from the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllLevel3(c *C) {
	c.Assert(update.SecureMkdirAll("/path/to/something", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "path" 0755`,
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`fchown 4 123 456`,
		`mkdirat 4 "to" 0755`,
		`openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 5
		`fchown 5 123 456`,
		`close 4`,
		`close 3`,
		`mkdirat 5 "something" 0755`,
		`openat 5 "something" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`fchown 3 123 456`,
		`close 3`,
		`close 5`,
	})
}

// Ensure that we can detect read only filesystems.
func (s *utilsSuite) TestSecureMkdirAllROFS(c *C) {
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST) // just realistic
	s.sys.InsertFault(`mkdirat 4 "path" 0755`, syscall.EROFS)
	err := update.SecureMkdirAll("/rofs/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot operate on read-only filesystem at /rofs`)
	c.Assert(err.(*update.ReadOnlyFsError).Path, Equals, "/rofs")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,        // -> 3
		`mkdirat 3 "rofs" 0755`,                              // -> EEXIST
		`openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`close 3`,
		`mkdirat 4 "path" 0755`, // -> EROFS
		`close 4`,
	})
}

// Ensure that we don't chown existing directories.
func (s *utilsSuite) TestSecureMkdirAllExistingDirsDontChown(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "path" 0755`, syscall.EEXIST)
	err := update.SecureMkdirAll("/abs/path", 0755, 123, 456)
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "abs" 0755`,
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`close 3`,
		`mkdirat 4 "path" 0755`,
		`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`close 3`,
		`close 4`,
	})
}

// Ensure that we we close everything when mkdirat fails.
func (s *utilsSuite) TestSecureMkdirAllMkdiratError(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, errTesting)
	err := update.SecureMkdirAll("/abs", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot mkdir path segment "abs": testing`)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "abs" 0755`,
		`close 3`,
	})
}

// Ensure that we we close everything when fchown fails.
func (s *utilsSuite) TestSecureMkdirAllFchownError(c *C) {
	s.sys.InsertFault(`fchown 4 123 456`, errTesting)
	err := update.SecureMkdirAll("/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot chown path segment "path" to 123.456 \(got up to "/"\): testing`)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "path" 0755`,
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`fchown 4 123 456`,
		`close 4`,
		`close 3`,
	})
}

// Check error path when we cannot open root directory.
func (s *utilsSuite) TestSecureMkdirAllOpenRootError(c *C) {
	s.sys.InsertFault(`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := update.SecureMkdirAll("/abs/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, "cannot open root directory: testing")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> err
	})
}

// Check error path when we cannot open non-root directory.
func (s *utilsSuite) TestSecureMkdirAllOpenError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := update.SecureMkdirAll("/abs/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot open path segment "abs" \(got up to "/"\): testing`)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "abs" 0755`,
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> err
		`close 3`,
	})
}

// realSystemSuite is not isolated / mocked from the system.
type realSystemSuite struct{}

var _ = Suite(&realSystemSuite{})

// Check that we can actually create directories.
// This doesn't test the chown logic as that requires root.
func (s *realSystemSuite) TestSecureMkdirAllForReal(c *C) {
	d := c.MkDir()

	// Create d (which already exists) with mode 0777 (but c.MkDir() used 0700
	// internally and since we are not creating the directory we should not be
	// changing that.
	c.Assert(update.SecureMkdirAll(d, 0777, -1, -1), IsNil)
	fi, err := os.Stat(d)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0700))

	// Create d1, which is a simple subdirectory, with a distinct mode and
	// check that it was applied. Note that default umask 022 is subtracted so
	// effective directory has different permissions.
	d1 := filepath.Join(d, "subdir")
	c.Assert(update.SecureMkdirAll(d1, 0707, -1, -1), IsNil)
	fi, err = os.Stat(d1)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0705))

	// Create d2, which is a deeper subdirectory, with another distinct mode
	// and check that it was applied.
	d2 := filepath.Join(d, "subdir/subdir/subdir")
	c.Assert(update.SecureMkdirAll(d2, 0750, -1, -1), IsNil)
	fi, err = os.Stat(d2)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0750))
}

// secure-mkfile-all

// Ensure that we refuse to create a directory with an relative path.
func (s *utilsSuite) TestSecureMkfileAllRelative(c *C) {
	err := update.SecureMkfileAll("rel/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot create file with relative path: "rel/path"`)
	c.Assert(s.sys.Calls(), HasLen, 0)
}

// Ensure that we can create a directory with an absolute path.
func (s *utilsSuite) TestSecureMkfileAllAbsolute(c *C) {
	c.Assert(update.SecureMkfileAll("/abs/path", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "abs" 0755`,
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 4 123 456`,
		`close 3`,
		`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_RDWR|O_CREAT|O_EXCL 0644`,
		`fchown 3 123 456`,
		`close 3`,
		`close 4`,
	})
}

// Ensure that we can detect read only filesystems.
func (s *utilsSuite) TestSecureMkfileAllROFS(c *C) {
	s.sys.InsertFault(`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_RDWR|O_CREAT|O_EXCL 0644`, syscall.EROFS)
	err := update.SecureMkfileAll("/rofs/path", 0755, 123, 456)
	c.Check(err, ErrorMatches, `cannot operate on read-only filesystem at /rofs`)
	c.Check(err.(*update.ReadOnlyFsError).Path, Equals, "/rofs")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "rofs" 0755`,
		`openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 4 123 456`,
		`close 3`,
		`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_RDWR|O_CREAT|O_EXCL 0644`,
		`close 4`,
	})
}

// Ensure that we don't chown existing files or directories.
func (s *utilsSuite) TestSecureMkfileAllExistingDirsDontChown(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_RDWR|O_CREAT|O_EXCL 0644`, syscall.EEXIST)
	err := update.SecureMkfileAll("/abs/path", 0755, 123, 456)
	c.Check(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "abs" 0755`,
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 3`,
		`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_RDWR|O_CREAT|O_EXCL 0644`,
		`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_RDWR 0`,
		`close 3`,
		`close 4`,
	})
}

// Ensure that we we close everything when mkdir fails.
func (s *utilsSuite) TestSecureMkfileAllCloseOnError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_RDWR|O_CREAT|O_EXCL 0644`, errTesting)
	err := update.SecureMkfileAll("/abs", 0755, 123, 456)
	c.Check(err, ErrorMatches, `cannot open file "abs": testing`)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_RDWR|O_CREAT|O_EXCL 0644`,
		`close 3`,
	})
}

func (s *utilsSuite) TestPlanWritableMimic(c *C) {
	restore := update.MockReadDir(func(dir string) ([]os.FileInfo, error) {
		c.Assert(dir, Equals, "/foo")
		return []os.FileInfo{
			update.FakeFileInfo("file", 0),
			update.FakeFileInfo("dir", os.ModeDir),
			// TODO: support symlinks
			update.FakeFileInfo("symlink", os.ModeSymlink),
			// NOTE: None of the filesystem entries below are supported because
			// they cannot be placed inside snaps or can only be created at
			// runtime in areas that are already writable and this would never
			// have to be handled in a writable mimic.
			update.FakeFileInfo("block-dev", os.ModeDevice),
			update.FakeFileInfo("char-dev", os.ModeDevice|os.ModeCharDevice),
			update.FakeFileInfo("socket", os.ModeSocket),
			update.FakeFileInfo("pipe", os.ModeNamedPipe),
		}, nil
	})
	defer restore()
	restore = update.MockReadlink(func(name string) (string, error) {
		c.Assert(name, Equals, "/foo/symlink")
		return "target", nil
	})
	defer restore()

	changes, err := update.PlanWritableMimic("/foo")
	c.Assert(err, IsNil)
	c.Assert(changes, DeepEquals, []*update.Change{
		// Store /foo in /tmp.snap/foo while we set things up
		{Entry: mount.Entry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"bind"}}, Action: update.Mount},
		// Put a tmpfs over /foo
		{Entry: mount.Entry{Name: "none", Dir: "/foo", Type: "tmpfs"}, Action: update.Mount},
		// Bind mount files and directories over. Note that files are identified by x-snapd.kind=file option.
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "ro", "x-snapd.kind=file"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"bind", "ro"}}, Action: update.Mount},
		// Create symlinks
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"bind", "ro", "x-snapd.kind=symlink", "x-snapd.symlink=target"}}, Action: update.Mount},
		// Unmount the safe-keeping directory
		{Entry: mount.Entry{Name: "none", Dir: "/tmp/.snap/foo"}, Action: update.Unmount},
	})
}
