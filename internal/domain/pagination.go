package domain

import "fmt"

// PageQuery drives pagination for list endpoints.
type PageQuery struct {
	Page      int
	PageSize  int
	SortBy    string
	SortOrder SortOrder
	Filter    map[string]string
}

// DefaultPage returns a PageQuery with safe defaults.
func DefaultPage() PageQuery {
	return PageQuery{
		Page:      1,
		PageSize:  20,
		SortOrder: SortDesc,
		Filter:    make(map[string]string),
	}
}

// Validate checks that page and page-size are within sane bounds.
func (q *PageQuery) Validate(maxPageSize int) error {
	if q.Page < 1 {
		return fmt.Errorf("page must be >= 1, got %d", q.Page)
	}
	if q.PageSize < 1 {
		return fmt.Errorf("page_size must be >= 1, got %d", q.PageSize)
	}
	if maxPageSize > 0 && q.PageSize > maxPageSize {
		return fmt.Errorf("page_size %d exceeds max %d", q.PageSize, maxPageSize)
	}
	return nil
}

// Offset returns the SQL OFFSET value for this page.
func (q *PageQuery) Offset() int {
	return (q.Page - 1) * q.PageSize
}

// PageResult wraps a page of items with total count and navigation metadata.
type PageResult[T any] struct {
	Items      []T
	Total      int
	Page       int
	PageSize   int
	TotalPages int
	HasNext    bool
	HasPrev    bool
}

// NewPageResult builds a PageResult, computing pagination metadata.
func NewPageResult[T any](items []T, total, page, pageSize int) PageResult[T] {
	totalPages := 0
	if pageSize > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	return PageResult[T]{
		Items:      items,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
		HasNext:    page < totalPages,
		HasPrev:    page > 1,
	}
}
