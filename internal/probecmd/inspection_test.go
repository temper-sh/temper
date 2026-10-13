package probecmd

import (
	"strings"
	"testing"
)

func inspectionFixture() (Invocation, processRow, processRow) {
	inv := Invocation{Path: "/owned/router", EnginePath: "/owned/engine"}
	router := processRow{pid: 100, ppid: 1, pgid: 100, started: "start", executable: inv.Path, state: "S"}
	helper := processRow{pid: 101, ppid: 100, pgid: 100, started: "helper start", executable: "/usr/sbin/system_profiler", state: "S",
		arguments: "system_profiler\x00-json\x00SPHardwareDataType\x00SPDisplaysDataType"}
	return inv, router, helper
}

func TestRouterInspectionRetainsOwnedGroupsWithoutBecomingEngine(t *testing.T) {
	inv, router, helper := inspectionFixture()
	child := processRow{pid: 102, ppid: helper.pid, pgid: 102, started: "child start", executable: helper.executable, state: "S",
		arguments: "system_profiler\x00-nospawn\x00-xml\x00SPHardwareDataType\x00-detailLevel\x00full"}
	known := map[int]processRow{}
	owned, roles, err := members([]processRow{child, helper, router}, 100, inv, known)
	if err != nil || len(owned) != 3 || len(roles) != 1 || roles[0].ID != "router" {
		t.Fatal(owned, roles, err)
	}
	// A helper may finish and a later inspection may use another PID. Neither
	// creates an engine role or relaxes the fixed engine identity contract.
	_, groups := processBoundary(nil, 100, known)
	if !groups[102] {
		t.Fatal("forgot the helper's independent shutdown group")
	}
	child.ppid = 1
	child.state, child.executable, child.arguments = "Z", "<defunct>", ""
	owned, roles, err = members([]processRow{router, child}, 100, inv, known)
	if err != nil || len(owned) != 2 || len(roles) != 1 || known[102].executable != helper.executable {
		t.Fatal("lost exiting helper identity", owned, roles, err)
	}
	helper.pid = 103
	if owned, roles, err = members([]processRow{router, helper}, 100, inv, known); err != nil || len(owned) != 2 || len(roles) != 1 {
		t.Fatal("refused a new inspection lifetime", owned, roles, err)
	}
}

func TestInspectionRefusesUnreviewedArgumentsPathsAndAncestry(t *testing.T) {
	for _, test := range []struct {
		name, path, args string
		parent, group    int
	}{
		{"sysctl write", "/usr/sbin/sysctl", "sysctl\x00-w\x00kern.maxproc=4096", 100, 100},
		{"sysctl disguised write", "/usr/sbin/sysctl", "sysctl\x00-n\x00kern.hv_vmm_present=1", 100, 100},
		{"unreviewed profiler", "/usr/sbin/system_profiler", "system_profiler\x00-xml\x00SPApplicationsDataType", 100, 100},
		{"missing args", "/usr/sbin/system_profiler", "", 100, 100},
		{"lookalike path", "/tmp/system_profiler", "system_profiler\x00-json\x00SPHardwareDataType\x00SPDisplaysDataType", 100, 100},
		{"unrelated group member", "/usr/sbin/system_profiler", "system_profiler\x00-json\x00SPHardwareDataType\x00SPDisplaysDataType", 1, 100},
		{"engine child", "/usr/sbin/sysctl", "sysctl\x00-n\x00kern.hv_vmm_present", 200, 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			inv, router, helper := inspectionFixture()
			helper.executable, helper.arguments, helper.ppid, helper.pgid = test.path, test.args, test.parent, test.group
			engine := processRow{pid: 200, ppid: 100, pgid: 100, started: "engine", executable: inv.EnginePath, state: "S"}
			if _, _, err := members([]processRow{router, engine, helper}, 100, inv, map[int]processRow{}); err == nil {
				t.Fatal("accepted unverified helper")
			}
		})
	}
}

func TestInspectionArgumentsRemainBoundAndUnknownZombiesStayUnknown(t *testing.T) {
	inv, router, helper := inspectionFixture()
	known := map[int]processRow{}
	if _, _, err := members([]processRow{router, helper}, 100, inv, known); err != nil {
		t.Fatal(err)
	}
	helper.arguments = strings.ReplaceAll(helper.arguments, "-json", "-xml")
	if _, _, err := members([]processRow{router, helper}, 100, inv, known); err == nil {
		t.Fatal("accepted changed argv")
	}
	helper.pid, helper.state, helper.executable = 103, "Z", "<defunct>"
	if _, _, err := members([]processRow{router, helper}, 100, inv, known); err == nil {
		t.Fatal("adopted unobserved zombie")
	}
}

func TestReviewedReadOnlyInspectionCommands(t *testing.T) {
	for _, row := range []processRow{
		{executable: "/usr/sbin/sysctl", arguments: "sysctl\x00-n\x00kern.hv_vmm_present"},
		{executable: "/usr/sbin/ioreg", arguments: "ioreg\x00-r\x00-c\x00IOGPU\x00-d\x001\x00-f"},
	} {
		if !inspectionCommand(row) {
			t.Fatal("refused reviewed command", row)
		}
	}
}

func TestInspectionCannotReuseAnIdentityOrEscapeItsBoundGroup(t *testing.T) {
	for _, change := range []string{"start", "group", "executable"} {
		t.Run(change, func(t *testing.T) {
			inv, router, helper := inspectionFixture()
			known := map[int]processRow{}
			if _, _, err := members([]processRow{router, helper}, 100, inv, known); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "start":
				helper.started = "new process"
			case "group":
				helper.pgid = helper.pid
			case "executable":
				helper.executable = "/usr/sbin/sysctl"
				helper.arguments = "sysctl\x00-n\x00kern.hv_vmm_present"
			}
			if _, _, err := members([]processRow{router, helper}, 100, inv, known); err == nil {
				t.Fatal("accepted changed inspection identity")
			}
		})
	}
}
