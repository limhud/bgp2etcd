package messages

import (
	"fmt"
	"net/netip"
	"strings"
)

type UpdateMessage struct {
	FullView bool // if set, it means that it contains the a full view.
	// we are using map of map in order to avoid duplicate prefixes.
	Additions map[netip.Addr]map[netip.Prefix]bool
	Deletions map[netip.Addr]map[netip.Prefix]bool
}

func NewUpdateMessage() *UpdateMessage {
	return &UpdateMessage{
		Additions: make(map[netip.Addr]map[netip.Prefix]bool),
		Deletions: make(map[netip.Addr]map[netip.Prefix]bool),
	}
}

func (m *UpdateMessage) String() string {
	str := []string{
		fmt.Sprintf("FullView: %t", m.FullView),
	}
	additions := []string{}
	for nextHop, prefixMap := range m.Additions {
		prefixesStr := []string{}
		for prefix := range prefixMap {
			prefixesStr = append(prefixesStr, prefix.String())
		}
		additions = append(additions, fmt.Sprintf("%s: [%s]", nextHop.String(), strings.Join(prefixesStr, ", ")))
	}
	str = append(str, fmt.Sprintf("Additions: {%s}", strings.Join(additions, ", ")))
	deletions := []string{}
	for nextHop, prefixMap := range m.Deletions {
		prefixesStr := []string{}
		for prefix := range prefixMap {
			prefixesStr = append(prefixesStr, prefix.String())
		}
		deletions = append(deletions, fmt.Sprintf("%s: [%s]", nextHop.String(), strings.Join(prefixesStr, ", ")))
	}
	str = append(str, fmt.Sprintf("Deletions: {%s}", strings.Join(deletions, ", ")))
	return strings.Join(str, ", ")
}

func (m *UpdateMessage) AddRoute(prefix netip.Prefix, nextHop netip.Addr) error {
	prefixMap := make(map[netip.Prefix]bool)
	if pMap, exist := m.Additions[nextHop]; exist {
		prefixMap = pMap
	}
	prefixMap[prefix] = true
	m.Additions[nextHop] = prefixMap
	return nil
}

func (m *UpdateMessage) DeleteRoute(prefix netip.Prefix, nextHop netip.Addr) error {
	prefixMap := make(map[netip.Prefix]bool)
	if pMap, exist := m.Deletions[nextHop]; exist {
		prefixMap = pMap
	}
	prefixMap[prefix] = true
	m.Deletions[nextHop] = prefixMap
	return nil
}

func (m *UpdateMessage) IsEmpty() bool {
	return len(m.Additions) == 0 && len(m.Deletions) == 0
}

func (m *UpdateMessage) SetFullView() {
	m.FullView = true
}

func (m *UpdateMessage) IsFullView() bool {
	return m.FullView
}

func (m *UpdateMessage) Merge(m2 *UpdateMessage) error {
	for nextHop, prefixMap2 := range m2.Additions {
		prefixMap, exist := m.Additions[nextHop]
		if !exist {
			prefixMap = make(map[netip.Prefix]bool)
		}
		for prefix := range prefixMap2 {
			prefixMap[prefix] = true
		}
		m.Additions[nextHop] = prefixMap
	}
	for nextHop, prefixMap2 := range m2.Deletions {
		prefixMap, exist := m.Deletions[nextHop]
		if !exist {
			prefixMap = make(map[netip.Prefix]bool)
		}
		for prefix := range prefixMap2 {
			prefixMap[prefix] = true
		}
		m.Deletions[nextHop] = prefixMap
	}
	return nil
}

func (m *UpdateMessage) GetAdditions() map[netip.Addr][]netip.Prefix {
	res := make(map[netip.Addr][]netip.Prefix)
	for nextHop, prefixMap := range m.Additions {
		prefixList := []netip.Prefix(nil)
		for prefix := range prefixMap {
			prefixList = append(prefixList, prefix)
		}
		res[nextHop] = prefixList
	}
	return res
}

func (m *UpdateMessage) GetDeletions() map[netip.Addr][]netip.Prefix {
	res := make(map[netip.Addr][]netip.Prefix)
	for nextHop, prefixMap := range m.Deletions {
		prefixList := []netip.Prefix(nil)
		for prefix := range prefixMap {
			prefixList = append(prefixList, prefix)
		}
		res[nextHop] = prefixList
	}
	return res
}
