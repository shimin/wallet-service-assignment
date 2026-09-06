CREATE TABLE IF NOT EXISTS wallets (
    wallet_id UUID PRIMARY KEY,
    balance NUMERIC(18,4) NOT NULL DEFAULT 0,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS transactions (
    id SERIAL PRIMARY KEY,
    request_id UUID,
    operation VARCHAR(20) NOT NULL,
    from_wallet UUID,
    to_wallet UUID,
    amount NUMERIC(18,4) NOT NULL,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);
