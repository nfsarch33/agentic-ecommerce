package inmemory

import (
	"sort"

	"github.com/nfsarch33/agentic-ecommerce/internal/domain/membership"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

func sortPlansByName(plans []membership.MembershipPlan) {
	sort.SliceStable(plans, func(i, j int) bool {
		return plans[i].Name() < plans[j].Name()
	})
}

func sortMembersByEmail(members []membership.Member) {
	sort.SliceStable(members, func(i, j int) bool {
		return members[i].Email() < members[j].Email()
	})
}

func sortSubscriptionsByCreatedAt(subs []membership.Subscription) {
	sort.SliceStable(subs, func(i, j int) bool {
		return subs[i].CreatedAt().Before(subs[j].CreatedAt())
	})
}

func paginatePlans(plans []membership.MembershipPlan, page, perPage int) port.MembershipPlanList {
	page, perPage = normalisePagination(page, perPage)
	total := len(plans)
	start, end := pageBounds(total, page, perPage)
	return port.MembershipPlanList{Plans: plans[start:end], Total: total}
}

func paginateMembers(members []membership.Member, page, perPage int) port.MembershipMemberList {
	page, perPage = normalisePagination(page, perPage)
	total := len(members)
	start, end := pageBounds(total, page, perPage)
	return port.MembershipMemberList{Members: members[start:end], Total: total}
}

func paginateSubscriptions(subs []membership.Subscription, page, perPage int) port.MembershipSubscriptionList {
	page, perPage = normalisePagination(page, perPage)
	total := len(subs)
	start, end := pageBounds(total, page, perPage)
	return port.MembershipSubscriptionList{Subscriptions: subs[start:end], Total: total}
}

func normalisePagination(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	return page, perPage
}

func pageBounds(total, page, perPage int) (int, int) {
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	return start, end
}
