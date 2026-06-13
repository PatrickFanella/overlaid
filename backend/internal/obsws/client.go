package obsws

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	opHello           = 0
	opIdentify        = 1
	opIdentified      = 2
	opRequest         = 6
	opRequestResponse = 7

	defaultTimeout = 10 * time.Second
)

var errOBSAuthFailed = errors.New("obs websocket authentication failed")

type Client struct {
	URL      string
	Password string
	Dialer   *websocket.Dialer
}

type Status struct {
	Configured          bool   `json:"configured"`
	URL                 string `json:"url"`
	Reachable           bool   `json:"reachable"`
	Authenticated       bool   `json:"authenticated"`
	OBSStudioVersion    string `json:"obsStudioVersion,omitempty"`
	OBSWebSocketVersion string `json:"obsWebSocketVersion,omitempty"`
	RPCVersion          int    `json:"rpcVersion,omitempty"`
	Error               string `json:"error,omitempty"`
}

type SceneList struct {
	CurrentProgramSceneName string  `json:"currentProgramSceneName"`
	Scenes                  []Scene `json:"scenes"`
}

type Scene struct {
	SceneIndex int    `json:"sceneIndex"`
	SceneName  string `json:"sceneName"`
	SceneUUID  string `json:"sceneUuid,omitempty"`
}

type Input struct {
	InputName string `json:"inputName"`
	InputKind string `json:"inputKind,omitempty"`
	InputUuid string `json:"inputUuid,omitempty"`
}

type SceneItem struct {
	SceneItemID      int    `json:"sceneItemId"`
	SourceName       string `json:"sourceName"`
	SceneItemEnabled bool   `json:"sceneItemEnabled"`
}

type envelope struct {
	Op   int             `json:"op"`
	Data json.RawMessage `json:"d"`
}

type helloData struct {
	OBSStudioVersion    string          `json:"obsStudioVersion"`
	OBSWebSocketVersion string          `json:"obsWebSocketVersion"`
	RPCVersion          int             `json:"rpcVersion"`
	Authentication      *authentication `json:"authentication,omitempty"`
}

type authentication struct {
	Challenge string `json:"challenge"`
	Salt      string `json:"salt"`
}

type identifyData struct {
	RPCVersion     int    `json:"rpcVersion"`
	Authentication string `json:"authentication,omitempty"`
}

type requestData struct {
	RequestType string         `json:"requestType"`
	RequestID   string         `json:"requestId"`
	RequestData map[string]any `json:"requestData,omitempty"`
}

type requestResponse struct {
	RequestType   string         `json:"requestType"`
	RequestID     string         `json:"requestId"`
	RequestStatus requestStatus  `json:"requestStatus"`
	ResponseData  map[string]any `json:"responseData"`
}

type requestStatus struct {
	Result  bool   `json:"result"`
	Code    int    `json:"code"`
	Comment string `json:"comment"`
}

func (c Client) Status(ctx context.Context) Status {
	status := Status{Configured: strings.TrimSpace(c.URL) != "", URL: c.URL}
	if !status.Configured {
		status.Error = "obs websocket url is not configured"
		return status
	}

	conn, hello, err := c.connect(ctx)
	if err != nil {
		status.Error = err.Error()
		if errors.Is(err, errOBSAuthFailed) {
			status.Reachable = true
			status.Authenticated = false
			status.OBSStudioVersion = hello.OBSStudioVersion
			status.OBSWebSocketVersion = hello.OBSWebSocketVersion
			status.RPCVersion = hello.RPCVersion
			return status
		}
		return status
	}
	defer conn.Close()

	status.Reachable = true
	status.Authenticated = true
	status.OBSStudioVersion = hello.OBSStudioVersion
	status.OBSWebSocketVersion = hello.OBSWebSocketVersion
	status.RPCVersion = hello.RPCVersion
	return status
}

func (c Client) GetSceneList(ctx context.Context) (SceneList, error) {
	data, err := c.call(ctx, "GetSceneList", nil)
	if err != nil {
		return SceneList{}, err
	}

	var scenes SceneList
	encoded, err := json.Marshal(data)
	if err != nil {
		return SceneList{}, err
	}
	if err := json.Unmarshal(encoded, &scenes); err != nil {
		return SceneList{}, err
	}
	return scenes, nil
}

func (c Client) GetInputList(ctx context.Context) ([]Input, error) {
	data, err := c.call(ctx, "GetInputList", nil)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Inputs []Input `json:"inputs"`
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return nil, err
	}
	return payload.Inputs, nil
}

func (c Client) GetSceneItemList(ctx context.Context, sceneName string) ([]SceneItem, error) {
	sceneName = strings.TrimSpace(sceneName)
	if sceneName == "" {
		return nil, errors.New("sceneName is required")
	}

	data, err := c.call(ctx, "GetSceneItemList", map[string]any{"sceneName": sceneName})
	if err != nil {
		return nil, err
	}

	var payload struct {
		SceneItems []SceneItem `json:"sceneItems"`
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return nil, err
	}
	return payload.SceneItems, nil
}

func (c Client) SetCurrentProgramScene(ctx context.Context, sceneName string) error {
	sceneName = strings.TrimSpace(sceneName)
	if sceneName == "" {
		return errors.New("sceneName is required")
	}
	_, err := c.call(ctx, "SetCurrentProgramScene", map[string]any{"sceneName": sceneName})
	return err
}

func (c Client) SetInputMute(ctx context.Context, inputName string, muted bool) error {
	inputName = strings.TrimSpace(inputName)
	if inputName == "" {
		return errors.New("inputName is required")
	}
	_, err := c.call(ctx, "SetInputMute", map[string]any{"inputName": inputName, "inputMuted": muted})
	return err
}

func (c Client) RefreshBrowserSource(ctx context.Context, inputName string) error {
	inputName = strings.TrimSpace(inputName)
	if inputName == "" {
		return errors.New("inputName is required")
	}
	_, err := c.call(ctx, "PressInputPropertiesButton", map[string]any{"inputName": inputName, "propertyName": "refreshnocache"})
	return err
}

func (c Client) SetSceneItemEnabled(ctx context.Context, sceneName string, sceneItemID int, enabled bool) error {
	sceneName = strings.TrimSpace(sceneName)
	if sceneName == "" {
		return errors.New("sceneName is required")
	}
	if sceneItemID <= 0 {
		return errors.New("sceneItemId must be positive")
	}
	_, err := c.call(ctx, "SetSceneItemEnabled", map[string]any{"sceneName": sceneName, "sceneItemId": sceneItemID, "sceneItemEnabled": enabled})
	return err
}

func (c Client) call(ctx context.Context, requestType string, payload map[string]any) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	conn, _, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline := deadlineFromContext(ctx)

	requestID := newRequestID()
	if err := setConnDeadline(conn, deadline); err != nil {
		return nil, err
	}
	if err := conn.WriteJSON(envelope{Op: opRequest, Data: mustJSON(requestData{RequestType: requestType, RequestID: requestID, RequestData: payload})}); err != nil {
		return nil, err
	}

	for {
		if err := setConnDeadline(conn, deadline); err != nil {
			return nil, err
		}
		var msg envelope
		if err := conn.ReadJSON(&msg); err != nil {
			return nil, err
		}
		if msg.Op != opRequestResponse {
			continue
		}

		var response requestResponse
		if err := json.Unmarshal(msg.Data, &response); err != nil {
			return nil, err
		}
		if response.RequestID != requestID {
			continue
		}
		if !response.RequestStatus.Result {
			comment := strings.TrimSpace(response.RequestStatus.Comment)
			if comment == "" {
				comment = fmt.Sprintf("OBS request failed with code %d", response.RequestStatus.Code)
			}
			return nil, errors.New(comment)
		}
		return response.ResponseData, nil
	}
}

func (c Client) connect(ctx context.Context) (*websocket.Conn, helloData, error) {
	if strings.TrimSpace(c.URL) == "" {
		return nil, helloData{}, errors.New("obs websocket url is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	deadline := deadlineFromContext(ctx)

	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	conn, _, err := dialer.DialContext(ctx, c.URL, http.Header{"Sec-WebSocket-Protocol": []string{"obswebsocket.json"}})
	if err != nil {
		return nil, helloData{}, err
	}
	if err := setConnDeadline(conn, deadline); err != nil {
		conn.Close()
		return nil, helloData{}, err
	}

	hello, err := readHello(conn)
	if err != nil {
		conn.Close()
		return nil, helloData{}, err
	}

	identify := identifyData{RPCVersion: hello.RPCVersion}
	if hello.Authentication != nil {
		if strings.TrimSpace(c.Password) == "" {
			conn.Close()
			return nil, hello, fmt.Errorf("%w: obs websocket password is required", errOBSAuthFailed)
		}
		identify.Authentication = ComputeAuthentication(c.Password, hello.Authentication.Salt, hello.Authentication.Challenge)
	}

	if err := setConnDeadline(conn, deadline); err != nil {
		conn.Close()
		return nil, hello, fmt.Errorf("%w: %v", errOBSAuthFailed, err)
	}
	if err := conn.WriteJSON(envelope{Op: opIdentify, Data: mustJSON(identify)}); err != nil {
		conn.Close()
		return nil, hello, fmt.Errorf("%w: %v", errOBSAuthFailed, err)
	}

	for {
		if err := setConnDeadline(conn, deadline); err != nil {
			conn.Close()
			return nil, hello, fmt.Errorf("%w: %v", errOBSAuthFailed, err)
		}
		var msg envelope
		if err := conn.ReadJSON(&msg); err != nil {
			conn.Close()
			return nil, hello, fmt.Errorf("%w: %v", errOBSAuthFailed, err)
		}
		if msg.Op == opIdentified {
			return conn, hello, nil
		}
	}
}

func readHello(conn *websocket.Conn) (helloData, error) {
	var msg envelope
	if err := conn.ReadJSON(&msg); err != nil {
		return helloData{}, err
	}
	if msg.Op != opHello {
		return helloData{}, errors.New("OBS did not send Hello")
	}
	var hello helloData
	if err := json.Unmarshal(msg.Data, &hello); err != nil {
		return helloData{}, err
	}
	return hello, nil
}

func deadlineFromContext(ctx context.Context) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline
	}
	return time.Now().Add(defaultTimeout)
}

func setConnDeadline(conn *websocket.Conn, deadline time.Time) error {
	if err := conn.SetReadDeadline(deadline); err != nil {
		return err
	}
	return conn.SetWriteDeadline(deadline)
}

func ComputeAuthentication(password, salt, challenge string) string {
	secretHash := sha256.Sum256([]byte(password + salt))
	secret := base64.StdEncoding.EncodeToString(secretHash[:])
	authHash := sha256.Sum256([]byte(secret + challenge))
	return base64.StdEncoding.EncodeToString(authHash[:])
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	return hex.EncodeToString(raw[:])
}
