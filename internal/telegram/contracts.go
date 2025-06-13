package telegram


type Plan struct {
	ID           uint
	Name         string
	Price        int 
	DurationDays int
}


type Repository interface {
	
	ListPlans() ([]Plan, error)

	
	RegisterUser(tgID int64, username string) error
}


type WGAgentClient interface {
	
	GenerateConfig(userID int64) (string, error)

	
	RevokeConfig(userID int64) error
}


type Payments interface {
	
	CreateInvoice(userID int64, planID uint) (paymentURL string, err error)

	
	VerifyPayment(invoiceID string) (paid bool, err error)
}
