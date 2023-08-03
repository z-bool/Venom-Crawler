package filter

import (
	"Venom-Crawler/pkg/crawlergo/model"
)

type FilterHandler interface {
	DoFilter(req *model.Request) bool
}
