package codegraph

import (
	"sort"
	"strings"
)

// The kinds of entry point. A kind is what calls in — a client over HTTP, a
// broker, Kong's request cycle, a scheduler — and so decides how the list
// groups it and what can be done with it: only an HTTP route can be tried in
// the API Client.
const (
	EntryKindHTTP     = "http"
	EntryKindGraphQL  = "graphql"
	EntryKindRPC      = "rpc"
	EntryKindMessage  = "message"
	EntryKindCamel    = "camel" // source component is not statically classified
	EntryKindSocket   = "socket"
	EntryKindSchedule = "schedule"
	// EntryKindPlugin is a Kong plugin phase: code that runs inside every
	// route the plugin is attached to, rather than serving one of its own.
	EntryKindPlugin = "plugin"
)

// verbKinds is the kind a verb implies, for an entry point whose analyzer did
// not say — older Java analyzers, and every index written before
// entry points had a kind.
var verbKinds = func() map[string]string {
	kinds := map[string]string{
		"MESSAGE": EntryKindMessage, "EVENT": EntryKindMessage,
		"GRPC":   EntryKindRPC,
		"SOCKET": EntryKindSocket,
		"QUERY":  EntryKindGraphQL, "MUTATION": EntryKindGraphQL, "SUBSCRIPTION": EntryKindGraphQL,
		"FIELD": EntryKindGraphQL, "REFERENCE": EntryKindGraphQL,
		"CRON": EntryKindSchedule, "INTERVAL": EntryKindSchedule, "TIMEOUT": EntryKindSchedule,
	}
	for _, phase := range kongPhases {
		kinds[strings.ToUpper(phase)] = EntryKindPlugin
	}
	return kinds
}()

// kongPhaseOrder is a phase's position in Kong's request cycle, 1-based.
func kongPhaseOrder(phase string) int {
	for i, p := range kongPhases {
		if strings.EqualFold(p, phase) {
			return i + 1
		}
	}
	return 0
}

// completeEntryPoints fills what an analyzer left empty, in place, and
// returns the slice. It runs on every read, so an index written before these
// fields existed — or by an older Java analyzer —
// reads the same as one written today. Anything already set is kept: the
// analyzer knew better.
func completeEntryPoints(entries []EntryPoint) []EntryPoint {
	javaStyle := make([]int, 0, len(entries))
	for i := range entries {
		ep := &entries[i]
		if ep.Kind == "" {
			ep.Kind = verbKinds[strings.ToUpper(ep.Verb)]
			if ep.Kind == "" {
				ep.Kind = EntryKindHTTP
			}
		}
		if ep.Kind == EntryKindPlugin && ep.Order == 0 {
			ep.Order = kongPhaseOrder(ep.Verb)
		}
		if ep.Owner != "" {
			if ep.Service == "" && strings.Contains(ep.EntryNode, ".<camel:") {
				javaStyle = append(javaStyle, i)
			}
			continue
		}
		if ep.Kind == EntryKindPlugin {
			// A phase's target is its plugin's name, which is the heading.
			ep.Owner, ep.Group = ep.Target, ep.Target
			continue
		}
		ep.Owner, ep.Group = classOfMember(ep.EntryNode)
		// Only a Java FQN has packages to name a service by. A module path
		// (an old TypeScript index) has slashes, and its dots are file names.
		if ep.Service == "" && !strings.Contains(ep.Owner, "/") {
			javaStyle = append(javaStyle, i)
		}
	}
	assignJavaServices(entries, javaStyle)
	return entries
}

// classOfMember is the class a member's node id declares it in —
// `com.acme.web.UsersController.getUser(java.lang.String)` is
// `com.acme.web.UsersController`, shown as `UsersController`. Cut at the
// parameter list first: parameter types have dots of their own. A class that
// shares its FQN with one in another module carries ` [module]`, kept in the
// owner (it is the key) and dropped from the heading.
func classOfMember(nodeID string) (owner, group string) {
	head := nodeID
	if open := strings.IndexByte(head, '('); open >= 0 {
		head = head[:open]
	}
	owner = head
	if dot := strings.LastIndexByte(head, '.'); dot >= 0 {
		owner = head[:dot]
	}
	fqn := owner
	if bracket := strings.LastIndex(fqn, " ["); bracket >= 0 && strings.HasSuffix(fqn, "]") {
		fqn = fqn[:bracket]
	}
	return owner, fqn[strings.LastIndexByte(fqn, '.')+1:]
}

// assignJavaServices sets Service on the entry points at idx: the package
// segment right after the prefix every owner shares. `com.acme.refund.web`
// and `com.acme.order.web` are the `refund` and `order` services. The Java
// analyzer does not attach a build module to an endpoint, and a package is
// what a reader recognises anyway. One service is none: left empty, so the
// reader shows no heading over everything.
func assignJavaServices(entries []EntryPoint, idx []int) {
	packages := make(map[string][]string)
	for _, i := range idx {
		owner := entries[i].Owner
		if _, ok := packages[owner]; ok {
			continue
		}
		fqn := owner
		if bracket := strings.LastIndex(fqn, " ["); bracket >= 0 {
			fqn = fqn[:bracket]
		}
		var pkg []string
		if dot := strings.LastIndexByte(fqn, '.'); dot >= 0 {
			pkg = strings.Split(fqn[:dot], ".")
		}
		packages[owner] = pkg
	}
	if len(packages) < 2 {
		return
	}
	all := make([][]string, 0, len(packages))
	for _, pkg := range packages {
		all = append(all, pkg)
	}
	shared := 0
	for {
		if shared >= len(all[0]) {
			break
		}
		same := true
		for _, pkg := range all {
			if shared >= len(pkg) || pkg[shared] != all[0][shared] {
				same = false
				break
			}
		}
		if !same {
			break
		}
		shared++
	}
	services := map[string]bool{}
	serviceOf := map[string]string{}
	for owner, pkg := range packages {
		service := ""
		switch {
		case shared < len(pkg):
			service = pkg[shared]
		case len(pkg) > 0:
			service = pkg[len(pkg)-1]
		}
		serviceOf[owner] = service
		services[service] = true
	}
	if len(services) < 2 {
		return
	}
	for _, i := range idx {
		entries[i].Service = serviceOf[entries[i].Owner]
	}
}

// sortEntryPoints is the order the list reads in, the same for every caller:
// by owner, then by the order that means something, then by target and verb.
func sortEntryPoints(entries []EntryPoint) {
	sort.SliceStable(entries, func(a, b int) bool {
		x, y := entries[a], entries[b]
		if x.Owner != y.Owner {
			return x.Owner < y.Owner
		}
		if x.Order != y.Order {
			return x.Order < y.Order
		}
		if x.Target != y.Target {
			return x.Target < y.Target
		}
		return x.Verb < y.Verb
	})
}
