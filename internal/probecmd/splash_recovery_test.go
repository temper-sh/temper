package probecmd

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
)

func splashRecoveryFixture() (Invocation, processRow, processRow, processRow) {
	inv := Invocation{Path: "/owned/router", FrontendPath: "/owned/python3.13", FrontendArguments: []string{"bootstrap", "/prepared", "--port", "10001"},
		EnginePath: "/owned/splash", EngineArguments: []string{"serve-native", "/prepared", "32768", "25769803776", "0"}}
	inv.EngineRelaunchArguments = append(slices.Clone(inv.EngineArguments), "--relaunch-context", "32768")
	router := processRow{pid: 100, ppid: 1, pgid: 100, started: "router", executable: inv.Path, state: "S", arguments: inv.Path}
	frontend := processRow{pid: 101, ppid: 100, pgid: 101, started: "frontend", executable: inv.FrontendPath, state: "S", arguments: kernelArgs(inv.FrontendPath, inv.FrontendArguments)}
	native := processRow{pid: 102, ppid: 101, pgid: 102, started: "first engine", executable: inv.EnginePath, state: "S", arguments: kernelArgs(inv.EnginePath, inv.EngineArguments)}
	return inv, router, frontend, native
}

func kernelArgs(path string, args []string) string {
	return strings.Join(append([]string{path}, args...), "\x00")
}

func TestSplashRestartInvalidatesExperimentButBindsReplacementForCleanup(t *testing.T) {
	for _, firstObserved := range []bool{true, false} {
		inv, router, frontend, native := splashRecoveryFixture()
		known := map[int]processRow{}
		if firstObserved {
			if _, _, err := members([]processRow{router, frontend, native}, router.pid, inv, known); err != nil {
				t.Fatal(err)
			}
		}
		replacement := native
		replacement.pid, replacement.pgid, replacement.started = 103, 103, "replacement"
		replacement.arguments = kernelArgs(inv.EnginePath, inv.EngineRelaunchArguments)
		owned, roles, err := members([]processRow{replacement, frontend, router}, router.pid, inv, known)
		if !errors.Is(err, errEngineRestarted) || len(owned) != 3 || len(roles) != 3 || known[103] != replacement {
			t.Fatal("restart must fail the experiment and retain verified cleanup authority", owned, roles, known, err)
		}
		// Bound children remain ours after the frontend exits. The failure stays
		// visible even if this is the first observation made during shutdown.
		replacement.ppid = 1
		owned, _, err = members([]processRow{router, replacement}, router.pid, inv, known)
		if !errors.Is(err, errEngineRestarted) || len(owned) != 2 {
			t.Fatal("verified replacement lost after reparenting", owned, err)
		}
		if owned, _, err := members(nil, router.pid, inv, known); err != nil || len(owned) != 0 {
			t.Fatal("completed shutdown still reports a live replacement", owned, err)
		}
	}
}

func TestSplashStartupRetryAlsoInvalidatesExperiment(t *testing.T) {
	inv, router, frontend, native := splashRecoveryFixture()
	known := map[int]processRow{}
	if _, _, err := members([]processRow{router, frontend, native}, router.pid, inv, known); err != nil {
		t.Fatal(err)
	}
	// Before the first Ready, Splash retries its original command.
	native.pid, native.pgid, native.started = 103, 103, "retry"
	if _, _, err := members([]processRow{router, frontend, native}, router.pid, inv, known); !errors.Is(err, errEngineRestarted) || known[103] != native {
		t.Fatal("startup retry was accepted as the same experiment", err)
	}
}

func TestSplashRelaunchNeverRelaxesCommandParentOrLifetimeChecks(t *testing.T) {
	for _, scenario := range []string{"context", "target", "extra argument", "wrong executable", "bypassed frontend", "changed frontend", "same PID", "unselected relaunch", "two engines", "shell bypass", "unrelated child"} {
		t.Run(scenario, func(t *testing.T) {
			inv, router, frontend, first := splashRecoveryFixture()
			known := map[int]processRow{}
			if _, _, err := members([]processRow{router, frontend, first}, router.pid, inv, known); err != nil {
				t.Fatal(err)
			}
			native := first
			native.pid, native.pgid, native.started = 103, 103, "replacement"
			args := slices.Clone(inv.EngineRelaunchArguments)
			switch scenario {
			case "context":
				args[len(args)-1] = "32767"
			case "target":
				args[1] = "/other"
			case "extra argument":
				args = append(args, "--arbitrary")
			case "wrong executable":
				native.executable = "/other/splash"
			case "bypassed frontend", "shell bypass":
				native.ppid = router.pid
				if scenario == "shell bypass" {
					shell := native
					shell.executable, shell.arguments = "/bin/sh", ""
					known[shell.pid] = shell
				}
			case "changed frontend":
				frontend.arguments += "\x00other.py"
			case "same PID":
				native.pid, native.pgid = first.pid, first.pgid
			case "unselected relaunch":
				inv.EngineRelaunchArguments = nil
			}
			native.arguments = kernelArgs(native.executable, args)
			rows := []processRow{router, frontend, native}
			if scenario == "two engines" {
				rows = append(rows, first)
			}
			if scenario == "unrelated child" {
				rows = append(rows, processRow{pid: 104, ppid: 101, pgid: 104, started: "unknown", executable: "/unknown", state: "S"})
			}
			before := maps.Clone(known)
			if _, _, err := members(rows, router.pid, inv, known); err == nil || errors.Is(err, errEngineRestarted) {
				t.Fatal("ownership defect became an owned restart", err)
			}
			if !maps.Equal(known, before) {
				t.Fatal("partial observation granted cleanup authority")
			}
		})
	}
}

func managedSplashCommands(inv Invocation) []ManagedCommand {
	return []ManagedCommand{
		{ID: "qwen", Path: inv.FrontendPath, Arguments: inv.FrontendArguments},
		{ID: "qwen/engine", Parent: "qwen", Path: inv.EnginePath, Arguments: inv.EngineArguments, RelaunchArguments: inv.EngineRelaunchArguments},
	}
}

func splashManagedReader(rows []processRow) managedInspector {
	find := func(pid int) (processRow, error) {
		for _, row := range rows {
			if row.pid == pid {
				return row, nil
			}
		}
		return processRow{}, errors.New("absent process")
	}
	return managedInspector{
		rows:       func() ([]processRow, error) { return slices.Clone(rows), nil },
		executable: func(pid int) (string, error) { r, e := find(pid); return r.executable, e },
		arguments:  func(pid int) (string, error) { r, e := find(pid); return r.arguments, e },
		listeners:  func([]processRow, int, string) (bool, error) { return true, nil },
	}
}

func TestManagedSplashAcceptsExactRelaunchAndRetainsItsActualIdentity(t *testing.T) {
	for _, relaunched := range []bool{false, true} {
		inv, router, frontend, native := splashRecoveryFixture()
		if relaunched {
			native.pid, native.pgid, native.started = 103, 103, "replacement"
			native.arguments = kernelArgs(inv.EnginePath, inv.EngineRelaunchArguments)
		}
		rows := []processRow{native, frontend, router} // Parent need not precede child.
		reader := splashManagedReader(rows)
		commands := managedSplashCommands(inv)
		observation, err := reader.snapshot(router.pid, inv.Path, nil, "127.0.0.1:18080", commands)
		if err != nil || len(observation.Identities) != 3 || !observation.Listening {
			t.Fatal(observation, err)
		}
		identity := observation.Identities[0]
		if identity.ID != "qwen/engine" || identity.Arguments != native.arguments || !ManagedIdentityAllowed(identity, commands) {
			t.Fatal("actual replacement identity was lost", identity)
		}
		root := ManagedIdentity{ProcessIdentity: ProcessIdentity{ID: "qwen", PID: frontend.pid, PGID: frontend.pgid, Started: frontend.started}, Path: frontend.executable, Arguments: frontend.arguments}
		loaded, err := observeManagedLoads(rows, []ManagedIdentity{root}, commands, nil, reader.executable, reader.arguments)
		if err != nil || len(loaded) != 2 || loaded[0] != identity {
			t.Fatal("recovery did not rediscover the exact live child", loaded, err)
		}
		identity.Arguments += "\x00unselected"
		if ManagedIdentityAllowed(identity, commands) {
			t.Fatal("altered persisted replacement grants signaling authority")
		}
	}
}

func TestManagedSplashRejectsRelaunchImpostorsInObservationAndRecovery(t *testing.T) {
	for _, scenario := range []string{"context", "target", "wrong executable", "bypassed frontend", "other frontend", "two engines", "legacy"} {
		t.Run(scenario, func(t *testing.T) {
			inv, router, frontend, native := splashRecoveryFixture()
			commands := managedSplashCommands(inv)
			args := slices.Clone(inv.EngineRelaunchArguments)
			switch scenario {
			case "context":
				args[len(args)-1] = "65536"
			case "target":
				args[1] = "/other-model"
			case "wrong executable":
				native.executable = "/other/splash"
			case "bypassed frontend":
				native.ppid, native.pgid = router.pid, frontend.pgid
			case "other frontend":
				frontend.arguments += "\x00other.py"
			case "legacy":
				commands[1].RelaunchArguments = nil
			}
			native.arguments = kernelArgs(native.executable, args)
			rows := []processRow{native, frontend, router}
			if scenario == "two engines" {
				second := native
				second.pid, second.pgid = 104, 104
				rows = append(rows, second)
			}
			reader := splashManagedReader(rows)
			if _, err := reader.snapshot(router.pid, inv.Path, nil, "127.0.0.1:18080", commands); err == nil {
				t.Fatal("unselected process became managed availability")
			}
			root := ManagedIdentity{ProcessIdentity: ProcessIdentity{ID: "qwen", PID: frontend.pid, PGID: frontend.pgid, Started: frontend.started}, Path: inv.FrontendPath, Arguments: kernelArgs(inv.FrontendPath, inv.FrontendArguments)}
			if _, err := observeManagedLoads(rows, []ManagedIdentity{root}, commands, nil, reader.executable, reader.arguments); err == nil {
				t.Fatal("unselected process became recovery authority")
			}
		})
	}
}

func TestManagedSplashCapturedChildRemainsOwnedAfterFrontendExit(t *testing.T) {
	for _, scenario := range []string{"same child", "missing capture", "other PID", "other start", "other group", "other executable", "other arguments", "other role"} {
		t.Run(scenario, func(t *testing.T) {
			inv, _, frontend, native := splashRecoveryFixture()
			// Splash's native child normally shares its frontend's group.
			native.pgid, native.ppid = frontend.pgid, 1
			native.arguments = kernelArgs(inv.EnginePath, inv.EngineRelaunchArguments)
			root := ManagedIdentity{ProcessIdentity: ProcessIdentity{ID: "qwen", PID: frontend.pid, PGID: frontend.pgid, Started: frontend.started}, Path: frontend.executable, Arguments: frontend.arguments}
			identity := ManagedIdentity{ProcessIdentity: ProcessIdentity{ID: "qwen/engine", PID: native.pid, PGID: native.pgid, Started: native.started}, Path: native.executable, Arguments: native.arguments}
			switch scenario {
			case "other PID":
				identity.PID++
			case "other start":
				identity.Started = "different lifetime"
			case "other group":
				identity.PGID++
			case "other executable":
				identity.Path = "/other/splash"
			case "other arguments":
				identity.Arguments += "\x00unselected"
			case "other role":
				identity.ID = "other/engine"
			}
			captured := []ManagedIdentity{identity}
			if scenario == "missing capture" {
				captured = nil
			}
			rows := []processRow{native}
			reader := splashManagedReader(rows)
			got, err := observeManagedLoads(rows, []ManagedIdentity{root}, managedSplashCommands(inv), captured, reader.executable, reader.arguments)
			if scenario == "same child" {
				if err != nil || len(got) != 1 || got[0] != identity {
					t.Fatal("captured child lost cleanup authority", got, err)
				}
			} else if err == nil {
				t.Fatal("unbound or changed orphan gained cleanup authority", got)
			}
		})
	}
}
