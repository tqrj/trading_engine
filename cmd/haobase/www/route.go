package www

import (
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/yzimhao/trading_engine/cmd/haobase/www/middle"
	"github.com/yzimhao/trading_engine/cmd/haobase/www/order"
	"github.com/yzimhao/trading_engine/utils"
)

func Run() {
	g := gin.New()
	router(g)
	g.Run(viper.GetString("haobase.http.listen"))
}

func router(r *gin.Engine) {
	r.Use(utils.CorsMiddleware())

	api := r.Group("/api/v1/base")
	{

		api.Use(middle.CheckLogin())

		//todo 登陆验证
		api.GET("/assets/recharge", assets_recharge)
		api.GET("/assets", assets_balance)

		api.POST("/order/create", order.Create)
		api.POST("/order/cancel", order.Cancel)
		api.GET("/order/hisotry", order.History)
		api.GET("/order/unfinished", order.Unfinished)
	}

}
