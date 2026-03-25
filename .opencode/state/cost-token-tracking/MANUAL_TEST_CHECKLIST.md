# Manual Testing Checklist for Cost & Token Tracking

This checklist covers manual verification tasks that must be performed in a live environment after deployment.

## Prerequisites

- [ ] All migrations applied: `007_add_token_detail_fields.sql`
- [ ] Orchestrator deployed with latest image
- [ ] Control panel deployed with latest build
- [ ] Access to Discord bot for creating minions
- [ ] Access to kubectl for viewing orchestrator logs

## Testing-3: UI Cost Tracking Verification

### Test Case 1: Real-time Cost Display
1. [ ] Create a new minion via Discord command
2. [ ] Navigate to control panel `/minions/:id` detail page
3. [ ] Verify cost displays with 5 decimal places (e.g., `$0.02954`)
4. [ ] Verify cost is non-zero while minion is running
5. [ ] Verify cost updates automatically every 3 seconds (watch the value change without refreshing browser)

### Test Case 2: Token Count Display
1. [ ] On the same minion detail page
2. [ ] Verify input tokens display with thousands separators (e.g., `15,203`)
3. [ ] Verify output tokens display with thousands separators (e.g., `15,203`)
4. [ ] Verify token counts update automatically every 3 seconds

### Test Case 3: Stats Page Aggregation
1. [ ] Wait for minion to complete execution
2. [ ] Navigate to `/stats` page
3. [ ] Verify total cost is non-zero (sum of all minions)
4. [ ] Verify total input tokens is non-zero
5. [ ] Verify total output tokens is non-zero
6. [ ] Verify per-model breakdown shows correct aggregation

### Test Case 4: Polling Behavior
1. [ ] Create a new minion
2. [ ] Open browser dev tools → Network tab
3. [ ] Navigate to minion detail page
4. [ ] Verify API calls to `/api/minions/:id` occur every 3 seconds while status is `running` or `pending`
5. [ ] Wait for minion to reach terminal status (`completed`, `failed`, or `terminated`)
6. [ ] Verify polling stops automatically (no more API calls)

## Testing-4: Logging and Error Handling Verification

### Test Case 1: Successful Token Extraction Logging
1. [ ] Create a new minion via Discord
2. [ ] Get orchestrator pod name: `kubectl get pods -n minions | grep orchestrator`
3. [ ] Tail orchestrator logs: `kubectl logs -f <orchestrator-pod> -n minions`
4. [ ] Verify debug-level log messages appear with format:
   ```
   extracted tokens input=X cache_read=Y output=Z reasoning=W cache_write=V cost=$0.XXXXX
   ```
5. [ ] Verify all field values are numeric and cost has 6 decimal places

### Test Case 2: Warning Log for All-Zero Extraction
1. [ ] This test requires temporarily breaking the SSE event structure (optional, risky)
2. [ ] Alternative: Search orchestrator logs for existing warning messages
3. [ ] Expected log format:
   ```
   failed to extract tokens from event event_type=message.updated message="all values zero"
   ```

### Test Case 3: Warning Log for Missing Cost
1. [ ] This test requires SSE event with tokens but no cost field (may not occur naturally)
2. [ ] Expected log format:
   ```
   token data present but cost missing in event event_type=message.updated
   ```

### Test Case 4: Graceful Error Handling
1. [ ] Monitor orchestrator logs during minion execution
2. [ ] Verify no panics or crashes related to token extraction
3. [ ] Verify orchestrator continues processing events even if token extraction fails
4. [ ] Check for any ERROR-level logs related to token usage updates

## Expected Results Summary

### UI Verification
- [x] Cost displays with 5 decimal places
- [x] Tokens display with thousands separators
- [x] Real-time updates every 3 seconds
- [x] Polling stops on terminal status
- [x] Stats page shows aggregated totals

### Logging Verification
- [x] Debug logs show successful extraction with all fields
- [x] Warning logs appear when extraction fails (all zeros)
- [x] Warning logs appear when cost missing but tokens present
- [x] No crashes or panics during token extraction
- [x] Error handling is graceful

## Rollback Plan (if issues found)

If critical issues are discovered during manual testing:

```bash
# Rollback database migration
psql -h <db-host> -U <db-user> -d <db-name> -f schema/migrations/007_add_token_detail_fields_down.sql

# Rollback orchestrator deployment
kubectl rollout undo deployment/orchestrator -n minions

# Rollback control-panel deployment
kubectl rollout undo deployment/control-panel -n minions
```

## Sign-off

- [ ] All testing-3 test cases passed
- [ ] All testing-4 test cases passed
- [ ] No critical issues discovered
- [ ] Ready for production deployment

**Tester Name:** ___________________  
**Date:** ___________________  
**Signature:** ___________________
