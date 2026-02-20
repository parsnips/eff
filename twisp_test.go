package eff

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Well-known IDs
var (
	journalID  = uuid.MustParse("b125f5a0-e803-11f0-a078-069b540ea27c")
	tranCodeID = uuid.MustParse("4e6acb34-7ecf-48d3-9892-df400be1998e")
	account1ID = uuid.MustParse("1fd1dd3e-33fe-4ef5-9d58-676ef8d306b5") // Ernie
	account2ID = uuid.MustParse("6c6affb0-5cf5-402b-8d84-01bfc1624a2c") // Bert
)

func TestPointInTimeEffectiveAndStatementDates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

	// Start Twisp container.
	//tc, err := StartTwisp(ctx, WithTestLogger(t))
	tc, err := StartTwisp(ctx)
	require.NoError(t, err, "StartTwisp")
	t.Cleanup(func() {
		tc.Cleanup(ctx, t)
		cancel()
	})

	client := tc.NewGraphQLClient(http.Header{
		"x-twisp-account-id": []string{uuid.New().String()},
	})

	t.Run("CreateActivityIndex", func(t *testing.T) {
		resp, err := CreateActivityIndex(ctx, client)
		require.NoError(t, err)
		require.Equal(t, "Entry", string(resp.Schema.CreateIndex.On))
	})

	t.Run("Setup", func(t *testing.T) {
		resp, err := Setup(ctx, client, journalID, tranCodeID, account1ID, account2ID)
		require.NoError(t, err)
		require.Equal(t, journalID, resp.CreateJournal.JournalId)
		require.Equal(t, tranCodeID, resp.CreateTranCode.TranCodeId)
		require.Equal(t, account1ID, resp.Ernie_checking.AccountId)
		require.Equal(t, account2ID, resp.Bert_checking.AccountId)
	})

	dates := []Date{
		NewDate(2026, time.January, 1),
		NewDate(2026, time.January, 15),
		NewDate(2026, time.January, 31),
		NewDate(2026, time.February, 15),
	}

	var closeStamp Timestamp
	for i, effective := range dates {
		t.Run("PostTransaction", func(t *testing.T) {
			txID := uuid.New()
			resp, err := PostTransaction(ctx, client, txID, effective)
			require.NoError(t, err)
			require.Equal(t, txID, resp.PostTransaction.TransactionId)
			// Set the closeStamp on the last january transaction
			if i == 2 {
				closeStamp = resp.GetPostTransaction().Created
			}
		})

	}

	janCloseStampStr := closeStamp.Time.Add(1 * time.Millisecond).Format(time.RFC3339Nano)

	// An "adjustment" transaction
	// effective in Jan _past_ the cutoff
	// statementDate in Feb
	t.Run("PostTransactionWithStatementDate", func(t *testing.T) {
		txID := uuid.New()
		effective := NewDate(2026, time.January, 24)
		statementDate := NewDate(2026, time.February, 15)
		resp, err := PostTransactionWithStatementDate(ctx, client, txID, effective, statementDate)
		require.NoError(t, err)
		require.Equal(t, txID, resp.PostTransaction.TransactionId)
	})

	t.Run("StatementBalanceJan", func(t *testing.T) {
		openDate := NewDate(2025, time.December, 31)
		closeDate := NewDate(2026, time.January, 31)

		resp, err := StatementBalance(
			ctx, client,
			account1ID, journalID,
			openDate, closeDate,
			// January effective cutoff
			janCloseStampStr, janCloseStampStr,
		)
		require.NoError(t, err)
		require.Equal(t, Decimal("0.00"), resp.Open.Available.NormalBalance.GetUnits())
		require.Equal(t, Decimal("3.00"), resp.Closed.Available.NormalBalance.GetUnits())
	})

	t.Run("StatementBalanceFeb", func(t *testing.T) {
		openDate := NewDate(2026, time.January, 31)
		closeDate := NewDate(2026, time.February, 28)

		resp, err := StatementBalance(
			ctx, client,
			account1ID, journalID,
			openDate, closeDate,
			janCloseStampStr,
			// Close for february in the future
			time.Now().Add(1*time.Hour).UTC().Format(time.RFC3339Nano),
		)
		require.NoError(t, err)

		require.Equal(t, Decimal("3.00"), resp.Open.Available.NormalBalance.GetUnits())
		require.Equal(t, Decimal("9.00"), resp.Closed.Available.NormalBalance.GetUnits())
	})

	t.Run("Activity Jan", func(t *testing.T) {
		resp, err := ActivityQuery(
			ctx,
			client,
			Ptr(journalID.String()),
			Ptr(account1ID.String()),
			Ptr("2026-01"),
		)

		require.NoError(t, err)
		require.NotNil(t, resp)

		expectedResp := ActivityQueryResponse{
			Entries: ActivityQueryEntriesEntryConnection{
				Nodes: []*ActivityQueryEntriesEntryConnectionNodesEntry{
					{
						Metadata: Ptr(map[string]any{
							"effective":     "2026-01-31",
							"statementDate": "2026-01-31",
						}),
						Amount: ActivityQueryEntriesEntryConnectionNodesEntryAmountMoney{
							Units: Decimal("1.00"),
						},
					},
					{
						Metadata: Ptr(map[string]any{
							"effective":     "2026-01-15",
							"statementDate": "2026-01-15",
						}),
						Amount: ActivityQueryEntriesEntryConnectionNodesEntryAmountMoney{
							Units: Decimal("1.00"),
						},
					},
					{
						Metadata: Ptr(map[string]any{
							"effective":     "2026-01-01",
							"statementDate": "2026-01-01",
						}),
						Amount: ActivityQueryEntriesEntryConnectionNodesEntryAmountMoney{
							Units: Decimal("1.00"),
						},
					},
				},
			},
		}

		require.EqualValues(t, string(Must(json.Marshal(expectedResp))), string(Must(json.Marshal(resp))))
	})

	t.Run("Activity Feb", func(t *testing.T) {
		resp, err := ActivityQuery(
			ctx,
			client,
			Ptr(journalID.String()),
			Ptr(account1ID.String()),
			Ptr("2026-02"),
		)

		require.NoError(t, err)
		require.NotNil(t, resp)

		expectedResp := ActivityQueryResponse{
			Entries: ActivityQueryEntriesEntryConnection{
				Nodes: []*ActivityQueryEntriesEntryConnectionNodesEntry{
					{
						Metadata: Ptr(map[string]any{
							"effective":     "2026-01-24",
							"statementDate": "2026-02-15",
						}),
						Amount: ActivityQueryEntriesEntryConnectionNodesEntryAmountMoney{
							Units: Decimal("5.00"),
						},
					},
					{
						Metadata: Ptr(map[string]any{
							"effective":     "2026-02-15",
							"statementDate": "2026-02-15",
						}),
						Amount: ActivityQueryEntriesEntryConnectionNodesEntryAmountMoney{
							Units: Decimal("1.00"),
						},
					},
				},
			},
		}
		require.JSONEq(t, string(Must(json.Marshal(expectedResp))), string(Must(json.Marshal(resp))))
	})
}

func TestParallelRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	// Start Twisp container.
	//tc, err := StartTwisp(ctx, WithTestLogger(t))
	tc, err := StartTwisp(ctx)
	require.NoError(t, err, "StartTwisp")
	t.Cleanup(
		func() {
			tc.Cleanup(ctx, t)
			cancel()
		},
	)

	var numRuns = 10
	runs := os.Getenv("RUNS")
	if runs != "" {
		numRuns, err = strconv.Atoi(runs)
		require.NoError(t, err)
	}

	for i := range numRuns {
		t.Run(fmt.Sprintf("test %d", i), func(tt *testing.T) {
			tt.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			tt.Cleanup(cancel)

			client := tc.NewGraphQLClient(http.Header{
				"x-twisp-account-id": []string{uuid.New().String()},
			})

			activityResp, err := CreateActivityIndex(ctx, client)
			require.NoError(tt, err)
			require.Equal(tt, "Entry", string(activityResp.Schema.CreateIndex.On))

			setupResp, err := Setup(ctx, client, journalID, tranCodeID, account1ID, account2ID)
			require.NoError(tt, err)
			require.Equal(tt, journalID, setupResp.CreateJournal.JournalId)
			require.Equal(tt, tranCodeID, setupResp.CreateTranCode.TranCodeId)
			require.Equal(tt, account1ID, setupResp.Ernie_checking.AccountId)
			require.Equal(tt, account2ID, setupResp.Bert_checking.AccountId)

			dates := []Date{
				NewDate(2026, time.January, 1),
				NewDate(2026, time.January, 15),
				NewDate(2026, time.January, 31),
				NewDate(2026, time.February, 15),
			}

			var closeStamp Timestamp
			for i, effective := range dates {
				txID := uuid.New()
				postResp, err := PostTransaction(ctx, client, txID, effective)
				require.NoError(tt, err)
				require.Equal(tt, txID, postResp.PostTransaction.TransactionId)
				// Set the closeStamp on the last january transaction
				if i == 2 {
					closeStamp = postResp.GetPostTransaction().Created
				}
			}

			janCloseStampStr := closeStamp.Time.Add(1 * time.Millisecond).Format(time.RFC3339Nano)

			// An "adjustment" transaction
			// effective in Jan _past_ the cutoff
			// statementDate in Feb

			txID := uuid.New()
			effective := NewDate(2026, time.January, 24)
			statementDate := NewDate(2026, time.February, 15)
			backdatedResp, err := PostTransactionWithStatementDate(ctx, client, txID, effective, statementDate)
			require.NoError(tt, err)
			require.Equal(tt, txID, backdatedResp.PostTransaction.TransactionId)

			openDate := NewDate(2025, time.December, 31)
			closeDate := NewDate(2026, time.January, 31)

			statementJanResp, err := StatementBalance(
				ctx, client,
				account1ID, journalID,
				openDate, closeDate,
				// January effective cutoff
				janCloseStampStr, janCloseStampStr,
			)
			require.NoError(tt, err)
			require.Equal(tt, Decimal("0.00"), statementJanResp.Open.Available.NormalBalance.GetUnits())
			require.Equal(tt, Decimal("3.00"), statementJanResp.Closed.Available.NormalBalance.GetUnits())

			openDate = NewDate(2026, time.January, 31)
			closeDate = NewDate(2026, time.February, 28)

			statementFebResp, err := StatementBalance(
				ctx, client,
				account1ID, journalID,
				openDate, closeDate,
				janCloseStampStr,
				// Close for february in the future
				time.Now().Add(1*time.Hour).UTC().Format(time.RFC3339Nano),
			)
			require.NoError(tt, err)

			require.Equal(tt, Decimal("3.00"), statementFebResp.Open.Available.NormalBalance.GetUnits())
			require.Equal(tt, Decimal("9.00"), statementFebResp.Closed.Available.NormalBalance.GetUnits())

			activityJanResp, err := ActivityQuery(
				ctx,
				client,
				Ptr(journalID.String()),
				Ptr(account1ID.String()),
				Ptr("2026-01"),
			)

			require.NoError(tt, err)
			require.NotNil(tt, activityJanResp)

			expectedJanResp := `{"entries":{"nodes":[{"metadata":{"effective":"2026-01-31","statementDate":"2026-01-31"},"amount":{"units":"1.00"},"transaction":{"metadata":{},"entries":{"nodes":[{"account":{"code":"ERNIE.CHECKING"}},{"account":{"code":"BERT.CHECKING"}}]}}},{"metadata":{"effective":"2026-01-15","statementDate":"2026-01-15"},"amount":{"units":"1.00"},"transaction":{"metadata":{},"entries":{"nodes":[{"account":{"code":"ERNIE.CHECKING"}},{"account":{"code":"BERT.CHECKING"}}]}}},{"metadata":{"effective":"2026-01-01","statementDate":"2026-01-01"},"amount":{"units":"1.00"},"transaction":{"metadata":{},"entries":{"nodes":[{"account":{"code":"ERNIE.CHECKING"}},{"account":{"code":"BERT.CHECKING"}}]}}}]}}`
			actualJanResp := string(Must(json.Marshal(activityJanResp)))

			require.JSONEq(tt, expectedJanResp, actualJanResp, actualJanResp)

			activityFebResp, err := ActivityQuery(
				ctx,
				client,
				Ptr(journalID.String()),
				Ptr(account1ID.String()),
				Ptr("2026-02"),
			)

			require.NoError(tt, err)
			require.NotNil(tt, activityFebResp)

			expectedFebResp := `{"entries":{"nodes":[{"metadata":{"effective":"2026-01-24","statementDate":"2026-02-15"},"amount":{"units":"5.00"},"transaction":{"metadata":{},"entries":{"nodes":[{"account":{"code":"ERNIE.CHECKING"}},{"account":{"code":"BERT.CHECKING"}}]}}},{"metadata":{"effective":"2026-02-15","statementDate":"2026-02-15"},"amount":{"units":"1.00"},"transaction":{"metadata":{},"entries":{"nodes":[{"account":{"code":"ERNIE.CHECKING"}},{"account":{"code":"BERT.CHECKING"}}]}}}]}}`
			actualFebResp := string(Must(json.Marshal(activityFebResp)))
			require.JSONEq(tt, expectedFebResp, actualFebResp, actualFebResp)
		})
	}
}

func Ptr[T any](t T) *T {
	return &t
}

func Must[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}
