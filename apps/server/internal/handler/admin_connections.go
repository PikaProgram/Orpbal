package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"Orpbal/ent"
	"Orpbal/ent/databaseconnection"
	"Orpbal/internal/api"
	"Orpbal/internal/security/dsn"
)

// ListDatabaseConnections returns every managed target.
func (h *Handler) ListDatabaseConnections(c *gin.Context) {
	connections, err := h.client.DatabaseConnection.Query().
		Order(ent.Asc(databaseconnection.FieldID)).
		All(c.Request.Context())
	if err != nil {
		h.fail(c, err, "list database connections")
		return
	}
	result := make([]api.DatabaseConnection, 0, len(connections))
	for _, connection := range connections {
		result = append(result, toAPIConnection(connection))
	}
	c.JSON(http.StatusOK, result)
}

// GetDatabaseConnection returns one managed target without credentials.
func (h *Handler) GetDatabaseConnection(c *gin.Context, id api.Id) {
	connection, err := h.client.DatabaseConnection.Get(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err, "get database connection")
		return
	}
	c.JSON(http.StatusOK, toAPIConnection(connection))
}

// CreateDatabaseConnection validates and saves a target after a live ping.
func (h *Handler) CreateDatabaseConnection(c *gin.Context, _ api.CreateDatabaseConnectionParams) {
	var req api.CreateConnectionRequest
	if !bindJSON(c, &req) {
		return
	}

	name := strings.TrimSpace(req.Name)
	host := strings.TrimSpace(req.Host)
	databaseName := strings.TrimSpace(req.DatabaseName)
	username := strings.TrimSpace(req.Username)
	port := 5432
	if req.Port != nil {
		port = *req.Port
	}
	sslMode := api.Disable
	if req.SslMode != nil {
		sslMode = *req.SslMode
	}
	if fields := validateConnection(name, host, databaseName, username, port, sslMode); fields != nil {
		writeFields(c, fields)
		return
	}

	plainPassword := ""
	if req.Password != nil {
		plainPassword = *req.Password
	}
	targetDSN, err := (dsn.Config{
		Host:         host,
		Port:         port,
		DatabaseName: databaseName,
		Username:     username,
		Password:     plainPassword,
		SSLMode:      string(sslMode),
	}).DSN()
	if err != nil {
		writeFields(c, map[string]string{"connection": err.Error()})
		return
	}
	if err := dsn.Ping(c.Request.Context(), targetDSN); err != nil {
		h.logger.Warn().Err(err).Str("connection", name).Msg("Target connection verification failed")
		writeError(c, http.StatusBadRequest, "connection_failed", "could not connect to the target database")
		return
	}

	create := h.client.DatabaseConnection.Create().
		SetName(name).
		SetHost(host).
		SetPort(port).
		SetDatabaseName(databaseName).
		SetUsername(username).
		SetSslMode(databaseconnection.SslMode(sslMode)).
		SetIsEnabled(true)
	if plainPassword != "" {
		encrypted, err := h.cipher.Encrypt([]byte(plainPassword))
		if err != nil {
			h.fail(c, err, "encrypt target password")
			return
		}
		create.SetPasswordCiphertext(encrypted)
	}

	connection, err := create.Save(c.Request.Context())
	if err != nil {
		h.fail(c, err, "create database connection")
		return
	}
	c.JSON(http.StatusCreated, toAPIConnection(connection))
}

// UpdateDatabaseConnection merges fields, verifies if enabled, and saves.
func (h *Handler) UpdateDatabaseConnection(c *gin.Context, id api.Id, _ api.UpdateDatabaseConnectionParams) {
	var req api.UpdateConnectionRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Name == nil && req.Host == nil && req.Port == nil && req.DatabaseName == nil &&
		req.Username == nil && req.Password == nil && req.SslMode == nil && req.IsEnabled == nil {
		writeFields(c, map[string]string{"body": "at least one field is required"})
		return
	}

	ctx := c.Request.Context()
	connection, err := h.client.DatabaseConnection.Get(ctx, id)
	if err != nil {
		h.fail(c, err, "get database connection for update")
		return
	}

	name := connection.Name
	host := connection.Host
	port := connection.Port
	databaseName := connection.DatabaseName
	username := connection.Username
	sslMode := api.SSLMode(connection.SslMode)
	enabled := connection.IsEnabled

	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if req.Host != nil {
		host = strings.TrimSpace(*req.Host)
	}
	if req.Port != nil {
		port = *req.Port
	}
	if req.DatabaseName != nil {
		databaseName = strings.TrimSpace(*req.DatabaseName)
	}
	if req.Username != nil {
		username = strings.TrimSpace(*req.Username)
	}
	if req.SslMode != nil {
		sslMode = *req.SslMode
	}
	if req.IsEnabled != nil {
		enabled = *req.IsEnabled
	}
	if fields := validateConnection(name, host, databaseName, username, port, sslMode); fields != nil {
		writeFields(c, fields)
		return
	}

	plainPassword, err := h.targetPassword(connection)
	if err != nil {
		h.fail(c, err, "decrypt target password")
		return
	}
	// Old DSN for pool invalidation: any query against the previous settings
	// must reconnect after the update.
	oldDSN, err := (dsn.Config{
		Host:         connection.Host,
		Port:         connection.Port,
		DatabaseName: connection.DatabaseName,
		Username:     connection.Username,
		Password:     plainPassword,
		SSLMode:      string(connection.SslMode),
	}).DSN()
	if err != nil {
		h.fail(c, err, "build previous target DSN")
		return
	}
	var encrypted []byte
	passwordChanged := false
	if req.Password != nil {
		passwordChanged = true
		plainPassword = *req.Password
		if plainPassword != "" {
			encrypted, err = h.cipher.Encrypt([]byte(plainPassword))
			if err != nil {
				h.fail(c, err, "encrypt target password")
				return
			}
		}
	}

	if enabled {
		targetDSN, err := (dsn.Config{
			Host:         host,
			Port:         port,
			DatabaseName: databaseName,
			Username:     username,
			Password:     plainPassword,
			SSLMode:      string(sslMode),
		}).DSN()
		if err != nil {
			writeFields(c, map[string]string{"connection": err.Error()})
			return
		}
		if err := dsn.Ping(ctx, targetDSN); err != nil {
			h.logger.Warn().Err(err).Str("connection", name).Msg("Target connection verification failed")
			writeError(c, http.StatusBadRequest, "connection_failed", "could not connect to the target database")
			return
		}
	}

	update := h.client.DatabaseConnection.UpdateOneID(connection.ID).
		SetName(name).
		SetHost(host).
		SetPort(port).
		SetDatabaseName(databaseName).
		SetUsername(username).
		SetSslMode(databaseconnection.SslMode(sslMode)).
		SetIsEnabled(enabled)
	if passwordChanged {
		if plainPassword == "" {
			update.ClearPasswordCiphertext()
		} else {
			update.SetPasswordCiphertext(encrypted)
		}
	}
	updated, err := update.Save(ctx)
	if err != nil {
		h.fail(c, err, "update database connection")
		return
	}
	h.viewer.Invalidate(oldDSN)
	c.JSON(http.StatusOK, toAPIConnection(updated))
}

func (h *Handler) targetPassword(connection *ent.DatabaseConnection) (string, error) {
	if len(connection.PasswordCiphertext) == 0 {
		return "", nil
	}
	plain, err := h.cipher.Decrypt(connection.PasswordCiphertext)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func toAPIConnection(connection *ent.DatabaseConnection) api.DatabaseConnection {
	return api.DatabaseConnection{
		Id:           connection.ID,
		Name:         connection.Name,
		Host:         connection.Host,
		Port:         connection.Port,
		DatabaseName: connection.DatabaseName,
		Username:     connection.Username,
		SslMode:      api.SSLMode(connection.SslMode),
		IsEnabled:    connection.IsEnabled,
		HasPassword:  len(connection.PasswordCiphertext) > 0,
		CreatedAt:    connection.CreatedAt,
		UpdatedAt:    connection.UpdatedAt,
	}
}

func validateConnection(name, host, databaseName, username string, port int, sslMode api.SSLMode) map[string]string {
	fields := map[string]string{}
	if name == "" {
		fields["name"] = "required"
	}
	if host == "" {
		fields["host"] = "required"
	}
	if databaseName == "" {
		fields["database_name"] = "required"
	}
	if username == "" {
		fields["username"] = "required"
	}
	if port < 1 || port > 65535 {
		fields["port"] = "must be between 1 and 65535"
	}
	if !sslMode.Valid() {
		fields["ssl_mode"] = fmt.Sprintf("must be one of %s", strings.Join([]string{"disable", "require", "verify-ca", "verify-full"}, ", "))
	}
	if len(fields) > 0 {
		return fields
	}
	return nil
}
