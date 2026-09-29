package probecmd

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestRapidCrashLogBindsCommandParentAndRetainsShutdownOwnership(t *testing.T) {
	// Own harmless fixture, not a copy of the upstream helper implementation.
	script := "import sys\nsys.stdin.buffer.read()\n"
	inv := Invocation{Path: "/owned/router", EnginePath: "/owned/python3.12",
		EngineArguments:      []string{"/owned/bin/rapid-mlx", "serve", "/model"},
		PythonCrashLogSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(script)))}
	router := processRow{100, 1, 100, "Tue Sep 22 12:00:00 2026", inv.Path, "S", ""}
	engine := processRow{101, 100, 101, router.started, inv.EnginePath, "S", inv.EnginePath + "\x00" + strings.Join(inv.EngineArguments, "\x00")}
	helper := processRow{102, 101, 102, router.started, inv.EnginePath, "S", inv.EnginePath + "\x00-c\x00" + script + "\x0014"}
	known := map[int]processRow{}
	for range 2 {
		owned, roles, err := members([]processRow{router, engine, helper}, 100, inv, known)
		if err != nil || len(owned) != 3 || len(roles) != 3 || !containsRole(roles, "crash-log") {
			t.Fatal("selected helper was not supervised", owned, roles, err)
		}
	}
	detached := helper
	detached.ppid = 1
	if owned, _, err := members([]processRow{detached}, 100, inv, known); err != nil || len(owned) != 1 {
		t.Fatal("lost helper ownership after engine exit", owned, err)
	}
	for _, tc := range []struct {
		name   string
		change func(*processRow)
	}{
		{"different interpreter", func(r *processRow) { r.executable = "/other/python3.12" }},
		{"changed script", func(r *processRow) { r.arguments = strings.Replace(r.arguments, "read()", "write(b'x')", 1) }},
		{"extra argument", func(r *processRow) { r.arguments += "\x00extra" }},
		{"standard descriptor", func(r *processRow) { r.arguments = strings.TrimSuffix(r.arguments, "14") + "2" }},
		{"invalid descriptor", func(r *processRow) { r.arguments = strings.TrimSuffix(r.arguments, "14") + "1x" }},
		{"router parent", func(r *processRow) { r.ppid = 100 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := helper
			tc.change(&bad)
			if _, _, err := members([]processRow{router, engine, bad}, 100, inv, map[int]processRow{}); err == nil {
				t.Fatal("unselected helper was admitted")
			}
		})
	}
	unselected := inv
	unselected.PythonCrashLogSHA256 = ""
	if _, _, err := members([]processRow{router, engine, helper}, 100, unselected, map[int]processRow{}); err == nil {
		t.Fatal("another engine admitted the Rapid helper")
	}
	shell := helper
	shell.executable, shell.arguments, shell.ppid = "/bin/sh", "", router.pid
	bypassed := helper
	bypassed.ppid = router.pid
	if _, _, err := members([]processRow{router, engine, bypassed}, 100, inv, map[int]processRow{shell.pid: shell}); err == nil {
		t.Fatal("an observed launch shell bypassed engine parent validation")
	}
	restarted := helper
	restarted.pid, restarted.pgid = 103, 103
	if _, _, err := members([]processRow{router, engine, restarted}, 100, inv, known); err == nil {
		t.Fatal("helper restart was admitted")
	}
	if _, _, err := members([]processRow{router, engine, helper, restarted}, 100, inv, map[int]processRow{}); err == nil {
		t.Fatal("multiple helpers were admitted")
	}
	restarted.ppid = helper.pid
	if _, _, err := members([]processRow{router, engine, helper, restarted}, 100, inv, map[int]processRow{}); err == nil {
		t.Fatal("helper's child was admitted as an engine-owned helper")
	}
}
