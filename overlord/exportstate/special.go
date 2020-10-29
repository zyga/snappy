// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package exportstate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// snapdTools returns an export set describing snapd tools.
//
// basePath is the path of the "$libexecdir/snapd" directory. This directory
// differs depending on who is providing the tools. Host with variable
// $libexecdir or snaps with a fixed one.
func snapdTools(basePath string) ExportSet {
	// tools contains the list of snapd tools to export.
	tools := []string{
		"etelpmoc.sh",         // used by tab completion logic
		"info",                // used by re-execution logic
		"snap-confine",        // sometimes used in special cases
		"snap-discard-ns",     // used by snap-confine inside the per-snap mount namespace
		"snap-exec",           // used by snap-confine inside the per-snap mount namespace
		"snap-gdb-shim",       // used by snap run --gdb
		"snap-gdbserver-shim", // used by snap run --experimental-gdbserver
		"snap-update-ns",      // used by snap-confine inside the per-snap mount namespace
		"snapctl",             // used by snaps to talk to snapd
	}
	set := ExportSet{
		Name:           "tools",
		ConsumerIsHost: false, // Those tools are for other snaps to use.
		Exports:        make(map[string]ExportedFile, len(tools)),
	}
	for _, tool := range tools {
		set.Exports[tool] = ExportedFile{
			Name:       tool,
			SourcePath: filepath.Join(basePath, tool),
		}
	}
	return set
}

// manifestForCoreSystem returns the manifest of the host as seen on ubuntu-core systems.
func manifestForCoreSystem() *Manifest {
	return &Manifest{
		ExportedName:    "snapd",
		ExportedVersion: "host",
		SourceIsHost:    true,
		// There are no export sets here, snapd snap is going to provide the tools.
	}
}

// manifestForClassicSystem returns the manifest of the host as seen on classic systems.
func manifestForClassicSystem() *Manifest {
	tools := snapdTools(dirs.DistroLibExecDir)
	return &Manifest{
		ExportedName:    "snapd",
		ExportedVersion: "host",
		SourceIsHost:    true,
		Sets:            map[string]ExportSet{tools.Name: tools},
	}
}

// manifestForSnapdSnap returns the manifest of the snapd snap.
func manifestForSnapdSnap(info *snap.Info) *Manifest {
	exportedName, exportedVersion := exportedNameVersionForSnapd(info)
	tools := snapdTools("/usr/lib/snapd")
	return &Manifest{
		ExportedName:     exportedName,
		ExportedVersion:  exportedVersion,
		SnapInstanceName: info.InstanceName(),
		SnapRevision:     info.Revision.String(),
		Sets:             map[string]ExportSet{tools.Name: tools},
	}
}

// manifestForCoreSnap returns the manifest of the core snap.
func manifestForCoreSnap(info *snap.Info) *Manifest {
	exportedName, exportedVersion := exportedNameVersionForCore(info)
	tools := snapdTools("/usr/lib/snapd")
	return &Manifest{
		ExportedName:     exportedName,
		ExportedVersion:  exportedVersion,
		SnapInstanceName: info.InstanceName(),
		SnapRevision:     info.Revision.String(),
		Sets:             map[string]ExportSet{tools.Name: tools},
	}
}

// manifestForRegularSnap returns the manifest for a snap other than core or snapd.
func manifestForRegularSnap(info *snap.Info) *Manifest {
	exportedName, exportedVersion := exportedNameVersionForRegularSnap(info)
	return &Manifest{
		ExportedName:     exportedName,
		ExportedVersion:  exportedVersion,
		SnapInstanceName: info.InstanceName(),
		SnapRevision:     info.Revision.String(),
		// TODO: eventually get this from the snap.yaml
	}
}

func exportedNameVersionForSnapd(info *snap.Info) (exportedName string, exportedVersion string) {
	return "snapd", info.Revision.String()
}

func exportedNameVersionForCore(info *snap.Info) (exportedName string, exportedVersion string) {
	return "snapd", fmt.Sprintf("core_%s", info.Revision)
}

func exportedNameVersionForRegularSnap(info *snap.Info) (exportedName string, exportedVersion string) {
	if typ := info.Type(); typ == snap.TypeSnapd || typ == snap.TypeOS {
		panic(fmt.Sprintf("internal error, cannot use exportedNameVersionForRegularSnap with core or snapd, got %q of type %q", info.SnapName(), info.Type()))
	}
	exportedName = info.SnapName() // Instance key goes to exportedVersion
	if info.InstanceKey == "" {
		exportedVersion = info.Revision.String()
	} else {
		exportedVersion = fmt.Sprintf("%s_%s", info.Revision.String(), info.InstanceKey)
	}
	return exportedName, exportedVersion
}

func effectiveExportedNameVersionForSnapdOrCore(st *state.State) (exportedName string, exportedVersion string, err error) {
	snapdInfo, coreInfo, err := currentSnapdAndCoreInfo(st)
	if err != nil {
		return "", "", err
	}
	var activeSnapdExportedVersion string
	var activeCoreExportedVersion string
	if snapdInfo != nil && snapdInfo.Broken == "" {
		exportedName, activeSnapdExportedVersion = exportedNameVersionForSnapd(snapdInfo)
	}
	if coreInfo != nil && coreInfo.Broken == "" {
		exportedName, activeCoreExportedVersion = exportedNameVersionForCore(coreInfo)
	}
	exportedVersion = selectExportedVersionForSnapdTools(activeSnapdExportedVersion, activeCoreExportedVersion)
	if exportedVersion != "" && exportedName == "" {
		exportedName = "snapd"
	}
	return exportedName, exportedVersion, nil
}

// selectExportedVersionForSnapdTools returns the version to use for snapd tools export set.
//
// The snapd tools export set is special as there are providers from snaps other
// than snapd that need consideration. The result is, in order of preference:
//
// 0) "host" if on classic with disabled re-execution.
// 1) snapd version, if available
// 2) core version, if available
// 3) "host" version, if on classic
//
// If no provider is available then empty version is returned.
func selectExportedVersionForSnapdTools(activeSnapdExportedVersion, activeCoreExportedVersion string) string {
	if release.OnClassic && os.Getenv("SNAP_REEXEC") == "0" {
		return "host"
	}
	if exportedVersion := activeSnapdExportedVersion; exportedVersion != "" {
		return exportedVersion
	}
	if exportedVersion := activeCoreExportedVersion; exportedVersion != "" {
		return exportedVersion
	}
	if release.OnClassic {
		return "host"
	}
	return ""
}

// currentSnapdAndCoreInfo returns infos of current revisions of snapd and core.
//
// If a given snap is not installed or does not have a current revision then
// nil returned in the corresponding place.
func currentSnapdAndCoreInfo(st *state.State) (snapdInfo *snap.Info, coreInfo *snap.Info, err error) {
	snapdInfo, err = snapstateCurrentInfo(st, "snapd")
	if _, ok := err.(*snap.NotInstalledError); err != nil && !ok {
		return nil, nil, err
	}
	coreInfo, err = snapstateCurrentInfo(st, "core")
	if _, ok := err.(*snap.NotInstalledError); err != nil && !ok {
		return nil, nil, err
	}
	return snapdInfo, coreInfo, nil
}

// snapstateCurrentInfo is snapstate.CurrentInfo mockable for testing.
var snapstateCurrentInfo = snapstate.CurrentInfo
