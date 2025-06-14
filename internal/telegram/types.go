package telegram

import "fmt"

// Command представляет команду бота
type Command string

const (
	CmdStart          Command = "start"
	CmdHelp           Command = "help"
	CmdPlans          Command = "plans"
	CmdAddPlan        Command = "addplan"
	CmdArchivePlan    Command = "archiveplan"
	CmdAddPMethod     Command = "addpmethod"
	CmdListPMethods   Command = "listpmethods"
	CmdArchivePMethod Command = "archivepmethod"
	CmdBuy            Command = "buy"
	CmdMyKeys         Command = "mykeys"
	CmdDisable        Command = "disable"
	CmdEnable         Command = "enable"
	CmdAdmins         Command = "admins"
	CmdPayQueue       Command = "payqueue"
	CmdInfo           Command = "info"
	CmdAddAdmin       Command = "add_admin"
	CmdRef            Command = "ref"
	CmdFeedback       Command = "feedback"
	CmdSupport        Command = "support"
)

func (c Command) String() string {
	return string(c)
}

func (c Command) IsValid() bool {
	switch c {
	case CmdStart, CmdHelp, CmdPlans, CmdAddPlan, CmdArchivePlan,
		CmdAddPMethod, CmdListPMethods, CmdArchivePMethod, CmdBuy,
		CmdMyKeys, CmdDisable, CmdEnable, CmdAdmins, CmdPayQueue,
		CmdInfo, CmdAddAdmin, CmdRef, CmdFeedback, CmdSupport:
		return true
	}
	return false
}

func (c Command) IsAdminOnly() bool {
	switch c {
	case CmdAddPlan, CmdArchivePlan, CmdAddPMethod, CmdListPMethods,
		CmdArchivePMethod, CmdDisable, CmdEnable, CmdAdmins,
		CmdPayQueue, CmdInfo, CmdAddAdmin:
		return true
	}
	return false
}

// AdminRole представляет роль администратора
type AdminRole string

const (
	RoleSuper   AdminRole = "super"
	RoleAdmin   AdminRole = "admin"
	RoleCashier AdminRole = "cashier"
	RoleSupport AdminRole = "support"
)

func (r AdminRole) String() string {
	return string(r)
}

func (r AdminRole) IsValid() bool {
	switch r {
	case RoleSuper, RoleAdmin, RoleCashier, RoleSupport:
		return true
	}
	return false
}

func (r AdminRole) DisplayName() string {
	switch r {
	case RoleSuper:
		return "суперадмин"
	case RoleAdmin:
		return "администратор"
	case RoleCashier:
		return "кассир"
	case RoleSupport:
		return "поддержка"
	}
	return "неизвестная роль"
}

func (r AdminRole) Emoji() string {
	switch r {
	case RoleSuper:
		return "👑"
	case RoleAdmin:
		return "⚡"
	case RoleCashier:
		return "💰"
	case RoleSupport:
		return "🎧"
	}
	return "👤"
}

func (r AdminRole) CanManageAdmins() bool {
	return r == RoleSuper
}

// PaymentStatus представляет статус платежа
type PaymentStatus string

const (
	PaymentStatusPending  PaymentStatus = "pending"
	PaymentStatusApproved PaymentStatus = "approved"
	PaymentStatusRejected PaymentStatus = "rejected"
)

func (s PaymentStatus) String() string {
	return string(s)
}

func (s PaymentStatus) IsValid() bool {
	switch s {
	case PaymentStatusPending, PaymentStatusApproved, PaymentStatusRejected:
		return true
	}
	return false
}

func (s PaymentStatus) DisplayName() string {
	switch s {
	case PaymentStatusPending:
		return "ожидает подтверждения"
	case PaymentStatusApproved:
		return "одобрен"
	case PaymentStatusRejected:
		return "отклонен"
	}
	return "неизвестный статус"
}

func (s PaymentStatus) Emoji() string {
	switch s {
	case PaymentStatusPending:
		return "⏳"
	case PaymentStatusApproved:
		return "✅"
	case PaymentStatusRejected:
		return "❌"
	}
	return "❓"
}

// Platform представляет платформу
type Platform string

const (
	PlatformAndroid Platform = "android"
	PlatformIOS     Platform = "ios"
	PlatformWindows Platform = "windows"
	PlatformLinux   Platform = "linux"
	PlatformMacOS   Platform = "macos"
)

func (p Platform) String() string {
	return string(p)
}

func (p Platform) IsValid() bool {
	switch p {
	case PlatformAndroid, PlatformIOS, PlatformWindows, PlatformLinux, PlatformMacOS:
		return true
	}
	return false
}

func (p Platform) DisplayName() string {
	switch p {
	case PlatformAndroid:
		return "Android"
	case PlatformIOS:
		return "iOS"
	case PlatformWindows:
		return "Windows"
	case PlatformLinux:
		return "Linux"
	case PlatformMacOS:
		return "macOS"
	}
	return "неизвестная платформа"
}

func (p Platform) Emoji() string {
	switch p {
	case PlatformAndroid:
		return "📱"
	case PlatformIOS:
		return "🍎"
	case PlatformWindows:
		return "🪟"
	case PlatformLinux:
		return "🐧"
	case PlatformMacOS:
		return "🍏"
	}
	return "💻"
}

// CallbackData представляет callback данные
type CallbackData string

const (
	CallbackAdminList    CallbackData = "admin_list"
	CallbackAdminAdd     CallbackData = "admin_add"
	CallbackAdminDisable CallbackData = "admin_disable"
	CallbackAdminCashier CallbackData = "admin_cashier"
)

func (c CallbackData) String() string {
	return string(c)
}

// CallbackPrefix представляет префиксы callback данных
type CallbackPrefix string

const (
	CallbackBuyPlan        CallbackPrefix = "buy_plan_"
	CallbackBuyPlatform    CallbackPrefix = "buy_platform_"
	CallbackBuyQty         CallbackPrefix = "buy_qty_"
	CallbackBuyMethod      CallbackPrefix = "buy_method_"
	CallbackPaymentApprove CallbackPrefix = "payment_approve_"
	CallbackPaymentReject  CallbackPrefix = "payment_reject_"
	CallbackInfoUser       CallbackPrefix = "info_user_"
	CallbackDisableAdmin   CallbackPrefix = "disable_admin_"
	CallbackSetCashier     CallbackPrefix = "set_cashier_"
	CallbackArchivePlan    CallbackPrefix = "archive_plan_"
	CallbackArchiveMethod  CallbackPrefix = "archive_method_"
	CallbackSubPlatform    CallbackPrefix = "sub_"
)

func (c CallbackPrefix) String() string {
	return string(c)
}

func (c CallbackPrefix) WithID(id interface{}) string {
	return string(c) + fmt.Sprintf("%v", id)
}

// BuyStep представляет шаг процесса покупки
type BuyStep string

const (
	BuyStepPlan     BuyStep = "plan"
	BuyStepPlatform BuyStep = "platform"
	BuyStepQty      BuyStep = "qty"
	BuyStepMethod   BuyStep = "method"
	BuyStepPayment  BuyStep = "payment"
	BuyStepReceipt  BuyStep = "receipt"
)

func (s BuyStep) String() string {
	return string(s)
}

func (s BuyStep) IsValid() bool {
	switch s {
	case BuyStepPlan, BuyStepPlatform, BuyStepQty, BuyStepMethod, BuyStepPayment, BuyStepReceipt:
		return true
	}
	return false
}

func (s BuyStep) Next() BuyStep {
	switch s {
	case BuyStepPlan:
		return BuyStepPlatform
	case BuyStepPlatform:
		return BuyStepQty
	case BuyStepQty:
		return BuyStepMethod
	case BuyStepMethod:
		return BuyStepPayment
	case BuyStepPayment:
		return BuyStepReceipt
	}
	return s
}

func (s BuyStep) DisplayName() string {
	switch s {
	case BuyStepPlan:
		return "выбор тарифа"
	case BuyStepPlatform:
		return "выбор платформы"
	case BuyStepQty:
		return "выбор количества"
	case BuyStepMethod:
		return "выбор способа оплаты"
	case BuyStepPayment:
		return "оплата"
	case BuyStepReceipt:
		return "подтверждение чека"
	}
	return "неизвестный шаг"
}
