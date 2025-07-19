The "catalog" package hard-codes currency and account data so that everything works without a DB for demo purposes.
In prod:
1. LookupAccount() would query the Accounts table in the DB.
2. Currency validation would rely on that table’s currency column.