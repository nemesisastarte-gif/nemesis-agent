package plugin

import (
	"github.com/samber/do"

	v1 "github.com/teteekoue/NemesisCode/backend/biz/plugin/handler/v1"
)

func ProvidePlugin(i *do.Injector) {
	do.Provide(i, v1.NewPluginHandler)
}

func InvokePlugin(i *do.Injector) {
	do.MustInvoke[*v1.PluginHandler](i)
}
