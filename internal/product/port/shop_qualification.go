package port

import "context"

// ShopQualificationCatalog exposes synchronized SKU identities, never saleability.
type ShopQualificationCatalog interface {
	ReadShopQualificationProduct(context.Context, ID) (ProductOption, error)
	ValidateShopQualificationProductsWithin(context.Context, []int64) error
	ListShopQualificationProducts(context.Context, ProductOptionQuery) (ProductOptionPage, error)
}
