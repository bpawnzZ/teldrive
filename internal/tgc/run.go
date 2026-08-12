package tgc

import (
	"context"
	"strings"
	"sync"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tgerr"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/internal/logging"
	"github.com/tgdrive/teldrive/internal/tgstorage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func RunWithAuth(ctx context.Context, client *telegram.Client, token string, f func(ctx context.Context) error) error {
	return client.Run(ctx, func(ctx context.Context) error {
		if err := Auth(ctx, client, token); err != nil {
			return err
		}
		return f(ctx)
	})
}

func Auth(ctx context.Context, client *telegram.Client, token string) error {
	status, err := client.Auth().Status(ctx)
	if err != nil {
		return err
	}

	logger := logging.Component("TG")

	if token == "" {
		if !status.Authorized {
			return errors.Errorf("not authorized. please login first")
		}
		logger.Debug("session.user",
			zap.Int64("user_id", status.User.ID),
			zap.String("username", status.User.Username))
	} else {
		if !status.Authorized {
			_, err := client.Auth().Bot(ctx, token)
			if err != nil {
				logger.Error("auth.bot_failed", zap.Error(err))
				return err
			}
			status, err = client.Auth().Status(ctx)
			if err != nil {
				return err
			}
			logger.Debug("session.bot",
				zap.Int64("bot_id", status.User.ID),
				zap.String("username", status.User.Username))
		}
	}
	return nil
}

// revokedKeyErrors are the Telegram RPC errors marking a bot's MTProto auth
// key as permanently dead. A revoked key cannot be repaired by retrying the
// same request; the bot must re-authenticate to mint a fresh key.
var revokedKeyErrors = []string{
	"AUTH_KEY_UNREGISTERED",
	"SESSION_EXPIRED",
	"AUTH_KEY_DUPLICATED",
}

// IsKeyRevocationError reports whether err indicates a permanently dead bot
// auth key (401 or one of the related terminal errors), which requires
// re-authentication rather than a plain retry.
func IsKeyRevocationError(err error) bool {
	if err == nil {
		return false
	}
	if rpcErr, ok := tgerr.As(err); ok {
		if rpcErr.Code == 401 || rpcErr.IsOneOf(revokedKeyErrors...) {
			return true
		}
	}
	// The error is wrapped outside the RPC layer (e.g. the retry middleware
	// tags it with "retry middleware skip"); fall back to a string match in
	// case the tgerr struct was not preserved through wrapping.
	msg := err.Error()
	if strings.Contains(msg, "rpc error code 401") {
		return true
	}
	for _, code := range revokedKeyErrors {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

// perBotAuthLocks serializes re-authentication per bot ID so concurrent
// parts sharing one bot key never race to mint new keys and revoke one
// another. Only one re-auth runs per bot at a time.
var perBotAuthLocks sync.Map // botID(string) -> *sync.Mutex

func botAuthLock(botID string) *sync.Mutex {
	v, _ := perBotAuthLocks.LoadOrStore(botID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// ReAuthBot permanently revokes the poisoned Telegram auth key for a bot by
// evicting its stored session and minting a fresh key via Auth().Bot(token).
//
// The operation is serialized per bot: a per-bot mutex guarantees a single
// active key per bot, and the freshly minted key is persisted to the session
// storage so any in-flight parts that were waiting on this re-auth reuse it.
// The returned client is fully authenticated and must be closed by the caller.
func ReAuthBot(ctx context.Context, db *gorm.DB, cache cache.Cacher, config *config.TGConfig, token string) (*telegram.Client, error) {
	botID := strings.Split(token, ":")[0]
	lock := botAuthLock(botID)
	lock.Lock()
	defer lock.Unlock()

	logger := logging.Component("TG")
	logger.Warn("auth.key_revoked.reauth", zap.String("bot_id", botID))

	// Drop the poisoned session (cache + DB) so the fresh client mints a new key.
	storage, err := tgstorage.NewSessionStorage(config.Session, db, cache, botID)
	if err != nil {
		return nil, errors.Wrap(err, "create session storage for re-auth")
	}
	if err := storage.Evict(ctx); err != nil {
		return nil, errors.Wrap(err, "evict poisoned session")
	}

	client, err := BotClient(ctx, db, cache, config, token)
	if err != nil {
		return nil, err
	}

	if err := RunWithAuth(ctx, client, token, func(ctx context.Context) error { return nil }); err != nil {
		_ = storage.Evict(ctx) // failed to mint: keep the row clean
		return nil, errors.Wrap(err, "mint fresh auth key")
	}

	return client, nil
}
