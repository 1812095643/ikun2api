package service

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// BalanceCreditSQL 允许管理员充值和抽奖发奖共用同一个事务内的余额、流水写入。
type BalanceCreditSQL interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// CreditAdminBalanceTx 的调用方负责提交事务；余额和流水任一写入未完成必须一起回滚。
func CreditAdminBalanceTx(ctx context.Context, tx BalanceCreditSQL, userID int64, amount, code, notes, source string) (int64, float64, error) {
	return creditBalanceTx(ctx, tx, userID, amount, code, notes, source, false)
}

// CreditCommittedBalanceTx 只用于已扣款活动的履约结算；账号停用或软删除不应阻塞整场已付款用户的奖励。
func CreditCommittedBalanceTx(ctx context.Context, tx BalanceCreditSQL, userID int64, amount, code, notes string) (int64, float64, error) {
	return creditBalanceTx(ctx, tx, userID, amount, code, notes, "lottery", true)
}

func creditBalanceTx(ctx context.Context, tx BalanceCreditSQL, userID int64, amount, code, notes, source string, includeDeleted bool) (int64, float64, error) {
	if len(amount) > 64 {
		return 0, 0, fmt.Errorf("充值金额过长")
	}
	value, err := decimal.NewFromString(amount)
	if err != nil || !value.IsPositive() || value.Exponent() < -8 || value.Exponent() > 12 || value.GreaterThan(decimal.NewFromInt(999999999999)) {
		return 0, 0, fmt.Errorf("充值金额应为大于零、最多八位小数的余额金额")
	}
	if source != "manual" && source != "gift" && source != "lottery" {
		return 0, 0, fmt.Errorf("充值来源无法识别")
	}
	filter := " AND deleted_at IS NULL"
	if includeDeleted {
		filter = ""
	}
	rows, err := tx.QueryContext(ctx, `UPDATE users SET balance=balance+$1::numeric,updated_at=NOW() WHERE id=$2`+filter+` RETURNING balance`, value.String(), userID)
	if err != nil {
		return 0, 0, err
	}
	var balance float64
	if !rows.Next() {
		err = rows.Err()
		rows.Close()
		if err == nil {
			err = ErrUserNotFound
		}
		return 0, 0, err
	}
	err = rows.Scan(&balance)
	closeErr := rows.Close()
	if err != nil {
		return 0, 0, err
	}
	if closeErr != nil {
		return 0, 0, closeErr
	}
	rows, err = tx.QueryContext(ctx, `INSERT INTO redeem_codes(code,type,value,status,used_by,used_at,notes,credit_source)
		VALUES($1,'admin_balance',$2::numeric,'used',$3,NOW(),$4,$5) RETURNING id`, code, value.String(), userID, notes, source)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	var recordID int64
	if !rows.Next() {
		return 0, 0, fmt.Errorf("充值流水未保存")
	}
	if err = rows.Scan(&recordID); err != nil {
		return 0, 0, err
	}
	return recordID, balance, rows.Err()
}

// InvalidateBalanceAfterCredit 必须在数据库提交后执行，避免网关继续使用旧余额。
func (s *adminServiceImpl) InvalidateBalanceAfterCredit(ctx context.Context, userID int64) {
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.billingCacheService != nil {
		_ = s.billingCacheService.InvalidateUserBalance(ctx, userID)
	}
}

func (s *adminServiceImpl) creditUserBalance(ctx context.Context, userID int64, amount float64, notes string) (*User, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 {
		return nil, fmt.Errorf("充值金额应大于零")
	}
	code, err := GenerateRedeemCode()
	if err != nil {
		return nil, err
	}
	tx, err := s.entClient.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	source := "manual"
	if strings.HasPrefix(notes, "[赠送]") {
		source = "gift"
	}
	_, _, err = CreditAdminBalanceTx(ctx, tx.Client(), userID, strconv.FormatFloat(amount, 'f', -1, 64), code, notes, source)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	s.InvalidateBalanceAfterCredit(cacheCtx, userID)
	if source == "manual" {
		s.tryAccrueAffiliateRebateForAdminRecharge(ctx, userID, "add", amount)
	}
	return s.userRepo.GetByID(ctx, userID)
}
