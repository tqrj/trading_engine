package orders

import (
	"errors"
	"fmt"
	"time"

	"github.com/yzimhao/trading_engine/cmd/haobase/base/varieties"
	"github.com/yzimhao/trading_engine/trading_core"
	"github.com/yzimhao/trading_engine/types/dbtables"
	"github.com/yzimhao/trading_engine/utils"
	"github.com/yzimhao/trading_engine/utils/app"
	"xorm.io/xorm"
)

type orderStatus int

const (
	OrderStatusNew    orderStatus = 0
	OrderStatusDone   orderStatus = 1
	OrderStatusCancel orderStatus = 2
)

// 委托记录表
type Order struct {
	Id             int64                  `xorm:"pk autoincr bigint" json:"id"`
	Symbol         string                 `xorm:"varchar(30)" json:"symbol"`
	OrderId        string                 `xorm:"varchar(30) unique(order_id) notnull" json:"order_id"`
	OrderSide      trading_core.OrderSide `xorm:"varchar(10) index(order_side)" json:"order_side"`
	OrderType      trading_core.OrderType `xorm:"varchar(10)" json:"order_type"` //价格策略，市价单，限价单
	UserId         string                 `xorm:"index(userid) notnull" json:"user_id"`
	Price          string                 `xorm:"decimal(40,20) notnull default(0)" json:"price"`
	Quantity       string                 `xorm:"decimal(40,20) notnull default(0)" json:"quantity"`
	FeeRate        string                 `xorm:"decimal(40,20) notnull default(0)" json:"fee_rate"`
	Amount         string                 `xorm:"decimal(40,20) notnull default(0)" json:"amount"`
	FreezeQty      string                 `xorm:"decimal(40,20) notnull default(0)" json:"freeze_qty"`
	FreezeAmount   string                 `xorm:"decimal(40,20) notnull default(0)" json:"freeze_amount"`
	AvgPrice       string                 `xorm:"decimal(40,20) notnull default(0)" json:"avg_price"` //订单撮合成功 结算逻辑写入的字段
	FinishedQty    string                 `xorm:"decimal(40,20) notnull default(0)" json:"finished_qty"`
	FinishedAmount string                 `xorm:"decimal(40,20) notnull default(0)" json:"finished_amount"`
	Fee            string                 `xorm:"decimal(40,20) notnull default(0)" json:"fee"`
	Status         orderStatus            `xorm:"tinyint(1) default(0)" json:"status"`
	CreateTime     int64                  `xorm:"bigint" json:"create_time"` //时间戳 精确到纳秒
	UpdateTime     utils.Time             `xorm:"timestamp updated" json:"update_time"`
}

func (o *Order) Save(db *xorm.Session) error {
	o.CreateTime = time.Now().UnixNano()
	if _, err := db.Table(o).Insert(o); err != nil {
		return err
	}
	return nil
}

func (o *Order) AutoCreateTable() error {
	db := app.Database().NewSession()
	defer db.Close()

	if !dbtables.Exist(db, o.TableName()) {
		err := db.CreateTable(o)
		if err != nil {
			return err
		}

		err = db.CreateIndexes(o)
		if err != nil {
			return err
		}

		err = db.CreateUniques(o)
		if err != nil {
			return err
		}
	}
	return nil
}

func (o *Order) TableName() string {
	return GetOrderTableName(o.Symbol)
}

func (o *Order) FormatDecimal(price_digit, qty_digit int) Order {
	o.Amount = utils.FormatDecimal(o.Amount, price_digit)
	o.AvgPrice = utils.FormatDecimal(o.AvgPrice, price_digit)
	o.Fee = utils.FormatDecimal(o.Fee, price_digit)
	o.FinishedAmount = utils.FormatDecimal(o.FinishedAmount, price_digit)
	o.Price = utils.FormatDecimal(o.Price, price_digit)

	o.Quantity = utils.FormatDecimal(o.Quantity, qty_digit)
	o.FinishedQty = utils.FormatDecimal(o.FinishedQty, qty_digit)

	o.FeeRate = utils.FormatDecimal(o.FeeRate, price_digit)
	o.FreezeQty = utils.FormatDecimal(o.FreezeQty, qty_digit)
	o.FreezeAmount = utils.FormatDecimal(o.FreezeAmount, price_digit)
	return *o
}

func GetOrderTableName(symbol string) string {
	return fmt.Sprintf("order_%s", symbol)
}

func Find(symbol string, order_id string) *Order {
	db := app.Database().NewSession()
	defer db.Close()

	var row Order
	db.Table(GetOrderTableName(symbol)).Where("order_id=?", order_id).Get(&row)
	if row.Id > 0 {
		return &row
	}
	return nil
}

// 订单预检
func order_pre_inspection(varieties *varieties.TradingVarieties, info *Order) (bool, error) {
	zero := utils.D("0")

	//下单数量的检查
	min_qty := utils.D(varieties.AllowMinQty.String())
	qty := utils.D(info.Quantity).Truncate(int32(varieties.QtyPrecision))
	if min_qty.Cmp(zero) > 0 && qty.Cmp(zero) > 0 && qty.Cmp(min_qty) < 0 {
		return false, errors.New("数量低于交易对最小限制")
	}

	//价格的检查
	price := utils.D(info.Price).Truncate(int32(varieties.PricePrecision))
	//?????
	if info.OrderType == trading_core.OrderTypeLimit && price.Cmp(zero) <= 0 {
		return false, errors.New("价格必须大于0")
	}

	//重置价格和数量
	info.Quantity = qty.String()
	info.Price = price.String()

	//下单金额的检查
	min_amount := utils.D(string(varieties.AllowMinAmount))
	amount := utils.D(info.Amount)
	if min_amount.Cmp(zero) > 0 && amount.Cmp(zero) > 0 && amount.Cmp(min_amount) < 0 {
		return false, errors.New("成交金额低于交易对最小限制")
	}

	//反向订单检查，不能让用户自己的订单撮合成交
	if info.OrderSide == trading_core.OrderSideBuy {

		//市价订单，检查市场反向是否有挂单
		if info.OrderType == trading_core.OrderTypeMarket {
			n := find_unfinished_orders_count(info.Symbol, trading_core.OrderSideSell)
			if n == 0 {
				return false, errors.New("市场无挂单")
			}
		}

		//检查卖单是否有挂单
		sell_orders := find_user_unfinished_orders(info.UserId, info.Symbol, trading_core.OrderSideSell)
		if len(sell_orders) > 0 {
			if (info.OrderType == trading_core.OrderTypeLimit && utils.D(sell_orders[0].Price).Cmp(utils.D(info.Price)) <= 0) || (info.OrderType == trading_core.OrderTypeMarket) {
				return false, errors.New("对向有挂单请撤单后再操作")
			}
		}
	} else if info.OrderSide == trading_core.OrderSideSell {
		//市价订单，检查市场反向是否有挂单
		if info.OrderType == trading_core.OrderTypeMarket {
			n := find_unfinished_orders_count(info.Symbol, trading_core.OrderSideBuy)
			if n == 0 {
				return false, errors.New("市场无挂单")
			}
		}

		buy_orders := find_user_unfinished_orders(info.UserId, info.Symbol, trading_core.OrderSideBuy)
		n := len(buy_orders)
		if n > 0 {
			if (info.OrderType == trading_core.OrderTypeLimit && utils.D(buy_orders[n-1].Price).Cmp(utils.D(info.Price)) >= 0) || (info.OrderType == trading_core.OrderTypeMarket) {
				return false, errors.New("对向有挂单请撤单后再操作")
			}
		}
	}

	return true, nil
}
