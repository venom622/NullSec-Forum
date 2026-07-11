package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var baseURL = func() string {
	if v := os.Getenv("HACKREDDIT_SERVER"); v != "" {
		return v
	}
	return "http://152.70.9.243:8080"
}()

var httpClient = &http.Client{Timeout: 10 * time.Second}

type Post struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	Content      string `json:"content"`
	Author       string `json:"author"`
	CreatedAt    string `json:"created_at"`
	Upvotes      int    `json:"upvotes"`
	Downvotes    int    `json:"downvotes"`
	CommentCount int    `json:"comment_count"`
}

type Comment struct {
	ID        int    `json:"id"`
	Content   string `json:"content"`
	Author    string `json:"author"`
	CreatedAt string `json:"created_at"`
	Upvotes   int    `json:"upvotes"`
	Downvotes int    `json:"downvotes"`
}

type AuthResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type Profile struct {
	ID           int      `json:"id"`
	Username     string   `json:"username"`
	CreatedAt    string   `json:"created_at"`
	Role         string   `json:"role"`
	Bio          string   `json:"bio"`
	Badges       []string `json:"badges"`
	PostCount    int      `json:"post_count"`
	CommentCount int      `json:"comment_count"`
}

type state int

const (
	stateSplash state = iota
	stateAuthMenu
	stateLogin
	stateRegister
	stateMainMenu
	statePostList
	statePostView
	stateNewPost
	stateNewComment
	stateSettings
	stateProfile
	stateEditBio
	stateAdminPanel
	stateAdminAction
	stateBadgeInput
)

const splashArt = `
     __       _ _ __               ___                          
  /\ \ \_   _| | / _\ ___  ___    / __\__  _ __ _   _ _ __ ___  
 /  \/ / | | | | \ \ / _ \/ __|  / _\/ _ \| '__| | | | '_ ` + "`" + ` _ \ 
/ /\  /| |_| | | |\ \  __/ (__  / / | (_) | |  | |_| | | | | | |
\_\ \/  \__,_|_|_\__/\___|\___| \/   \___/|_|   \__,_|_| |_| |_|
`

const splashSubtitle = "Made By 0x000"

type Theme struct {
	Name      string
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Accent    lipgloss.Color
	Muted     lipgloss.Color
}

var themes = []Theme{
	{"Matrix Green", lipgloss.Color("#57ff8f"), lipgloss.Color("#1fbf6b"), lipgloss.Color("#c8ffb7"), lipgloss.Color("#3b5832")},
	{"Cyber Purple", lipgloss.Color("#c084fc"), lipgloss.Color("#9333ea"), lipgloss.Color("#e9d5ff"), lipgloss.Color("#4c1d95")},
	{"Blood Red", lipgloss.Color("#ff5c5c"), lipgloss.Color("#e02424"), lipgloss.Color("#ffb3b3"), lipgloss.Color("#5c0f0f")},
	{"Ocean Blue", lipgloss.Color("#5cc8ff"), lipgloss.Color("#2196f3"), lipgloss.Color("#bfe9ff"), lipgloss.Color("#0a3d66")},
	{"Amber Gold", lipgloss.Color("#ffd166"), lipgloss.Color("#f4a300"), lipgloss.Color("#ffe9b3"), lipgloss.Color("#5c3d00")},
}

func buildSettingsMenu() menu {
	opts := make([]string, 0, len(themes)+1)
	for _, t := range themes {
		opts = append(opts, t.Name)
	}
	opts = append(opts, "Back")
	return menu{title: "Settings", options: opts}
}

type menu struct {
	title   string
	options []string
	cursor  int
}

func (m *menu) up() {
	if m.cursor > 0 {
		m.cursor--
	} else {
		m.cursor = len(m.options) - 1
	}
}

func (m *menu) down() {
	if m.cursor < len(m.options)-1 {
		m.cursor++
	} else {
		m.cursor = 0
	}
}

func (m menu) viewThemed(th Theme) string {
	titleStyle := lipgloss.NewStyle().Foreground(th.Primary).Bold(true)
	cursorStyle := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	s := titleStyle.Render(fmt.Sprintf("=== %s ===", m.title)) + "\n\n"
	for i, opt := range m.options {
		if i == m.cursor {
			s += cursorStyle.Render("> "+opt) + "\n"
		} else {
			s += "  " + opt + "\n"
		}
	}
	s += "\n(↑/↓ to move, Enter to select, q to quit)"
	return s
}

type form struct {
	title  string
	fields []textinput.Model
	focus  int
}

func newForm(title string, labels []string, masked []bool) form {
	fields := make([]textinput.Model, len(labels))
	for i, label := range labels {
		ti := textinput.New()
		ti.Placeholder = label
		ti.CharLimit = 500
		ti.Width = 40
		if masked[i] {
			ti.EchoMode = textinput.EchoPassword
			ti.EchoCharacter = '•'
		}
		if i == 0 {
			ti.Focus()
		}
		fields[i] = ti
	}
	return form{title: title, fields: fields}
}

func (f *form) next() {
	f.fields[f.focus].Blur()
	f.focus = (f.focus + 1) % len(f.fields)
	f.fields[f.focus].Focus()
}

func (f *form) prev() {
	f.fields[f.focus].Blur()
	f.focus = (f.focus - 1 + len(f.fields)) % len(f.fields)
	f.fields[f.focus].Focus()
}

func (f *form) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	f.fields[f.focus], cmd = f.fields[f.focus].Update(msg)
	return cmd
}

func (f form) value(i int) string {
	return f.fields[i].Value()
}

func (f form) view() string {
	s := fmt.Sprintf("=== %s ===\n\n", f.title)
	for i := range f.fields {
		s += f.fields[i].View() + "\n"
	}
	s += "\n(Tab/↑/↓ to switch fields, Enter to submit, Esc to go back)"
	return s
}

type model struct {
	state state

	splashFrame int

	authMenu   menu
	mainMenu   menu
	loginForm  form
	regForm    form
	postForm   form
	commentTI  textinput.Model

	list         *list.Model
	posts        []Post
	selectedPost *Post
	comments     []Comment

	err      error
	statusOK string
	token    string
	username string
	width    int
	height   int

	themeIdx     int
	settingsMenu menu

	myProfile         *Profile
	viewedProfile     *Profile
	viewProfileIsSelf bool
	bioForm           form

	adminList             *list.Model
	adminActionMenu       menu
	selectedAdminUsername string
	selectedAdminRole     string
	badgeForm             form
	badgeAction           string

	sidebarWidth int
}

func initialModel() model {
	m := model{
		state: stateSplash,
		authMenu: menu{
			title:   "NullSec Forum",
			options: []string{"Log in", "Register", "Quit"},
		},
		mainMenu: menu{
			title:   "Navigation",
			options: []string{"Browse posts", "New post", "My Profile", "Settings", "Log out", "Quit"},
		},
		loginForm:    newForm("Log In", []string{"Username", "Password"}, []bool{false, true}),
		regForm:      newForm("Register", []string{"Username", "Password"}, []bool{false, true}),
		postForm:     newForm("New Post", []string{"Title", "Content"}, []bool{false, false}),
		settingsMenu: buildSettingsMenu(),
		themeIdx:     0,
		sidebarWidth: 22,
	}
	return m
}

func (m model) buildMainMenuOptions() []string {
	opts := []string{"Browse posts", "New post", "My Profile", "Settings"}
	if m.myProfile != nil && m.myProfile.Role == "admin" {
		opts = append(opts, "Admin Panel")
	}
	opts = append(opts, "Log out", "Quit")
	return opts
}

func (m *model) refreshMainMenu() {
	m.mainMenu.options = m.buildMainMenuOptions()
	if m.mainMenu.cursor >= len(m.mainMenu.options) {
		m.mainMenu.cursor = len(m.mainMenu.options) - 1
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, tickCmd())
}

type postsLoaded struct{ posts []Post }
type postDetailsLoaded struct {
	post     Post
	comments []Comment
}
type postCreated struct{}
type commentCreated struct{}
type voteDone struct{ postID int }
type authSuccess struct {
	token    string
	username string
}
type registerSuccess struct {
	username string
	password string
}
type profileLoaded struct{ profile Profile }
type usersLoaded struct{ profiles []Profile }
type roleUpdated struct{}
type badgeUpdated struct{}
type errMsg struct{ error }
type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(70*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func doJSON(method, url string, token string, body interface{}, out interface{}) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewBuffer(b)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(msg))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (m model) login(username, password string) tea.Cmd {
	return func() tea.Msg {
		form := url.Values{}
		form.Set("grant_type", "password")
		form.Set("username", username)
		form.Set("password", password)
		req, _ := http.NewRequest("POST", baseURL+"/token", bytes.NewBufferString(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := httpClient.Do(req)
		if err != nil {
			return errMsg{err}
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return errMsg{fmt.Errorf("invalid username or password")}
		}
		var authResp AuthResponse
		if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
			return errMsg{err}
		}
		return authSuccess{token: authResp.AccessToken, username: username}
	}
}

func (m model) register(username, password string) tea.Cmd {
	return func() tea.Msg {
		payload := map[string]string{"username": username, "password": password}
		if err := doJSON("POST", baseURL+"/register", "", payload, nil); err != nil {
			return errMsg{err}
		}
		return registerSuccess{username: username, password: password}
	}
}

func (m model) loadPosts() tea.Cmd {
	return func() tea.Msg {
		var posts []Post
		if err := doJSON("GET", baseURL+"/posts", m.token, nil, &posts); err != nil {
			return errMsg{err}
		}
		return postsLoaded{posts}
	}
}

func (m model) loadPostDetails(postID int) tea.Cmd {
	return func() tea.Msg {
		var post Post
		if err := doJSON("GET", fmt.Sprintf("%s/posts/%d", baseURL, postID), m.token, nil, &post); err != nil {
			return errMsg{err}
		}
		var comments []Comment
		if err := doJSON("GET", fmt.Sprintf("%s/posts/%d/comments", baseURL, postID), m.token, nil, &comments); err != nil {
			return errMsg{err}
		}
		return postDetailsLoaded{post, comments}
	}
}

func (m model) createPost(title, content string) tea.Cmd {
	return func() tea.Msg {
		payload := map[string]string{"title": title, "content": content}
		if err := doJSON("POST", baseURL+"/posts", m.token, payload, nil); err != nil {
			return errMsg{err}
		}
		return postCreated{}
	}
}

func (m model) createComment(postID int, content string) tea.Cmd {
	return func() tea.Msg {
		payload := map[string]string{"content": content}
		url := fmt.Sprintf("%s/posts/%d/comments", baseURL, postID)
		if err := doJSON("POST", url, m.token, payload, nil); err != nil {
			return errMsg{err}
		}
		return commentCreated{}
	}
}

func (m model) votePost(postID int, value int) tea.Cmd {
	return func() tea.Msg {
		url := fmt.Sprintf("%s/vote?post_id=%d&value=%d", baseURL, postID, value)
		if err := doJSON("POST", url, m.token, nil, nil); err != nil {
			return errMsg{err}
		}
		return voteDone{postID}
	}
}

func (m model) loadProfile(username string) tea.Cmd {
	return func() tea.Msg {
		var profile Profile
		u := fmt.Sprintf("%s/users/%s", baseURL, url.PathEscape(username))
		if err := doJSON("GET", u, m.token, nil, &profile); err != nil {
			return errMsg{err}
		}
		return profileLoaded{profile}
	}
}

func (m model) updateBio(bio string) tea.Cmd {
	return func() tea.Msg {
		payload := map[string]string{"bio": bio}
		var profile Profile
		if err := doJSON("PATCH", baseURL+"/me", m.token, payload, &profile); err != nil {
			return errMsg{err}
		}
		return profileLoaded{profile}
	}
}

func (m model) loadAllUsers() tea.Cmd {
	return func() tea.Msg {
		var profiles []Profile
		if err := doJSON("GET", baseURL+"/admin/users", m.token, nil, &profiles); err != nil {
			return errMsg{err}
		}
		return usersLoaded{profiles}
	}
}

func (m model) setUserRole(username, role string) tea.Cmd {
	return func() tea.Msg {
		payload := map[string]string{"role": role}
		u := fmt.Sprintf("%s/admin/users/%s/role", baseURL, url.PathEscape(username))
		if err := doJSON("POST", u, m.token, payload, nil); err != nil {
			return errMsg{err}
		}
		return roleUpdated{}
	}
}

func (m model) addBadge(username, badge string) tea.Cmd {
	return func() tea.Msg {
		payload := map[string]string{"badge": badge}
		u := fmt.Sprintf("%s/admin/users/%s/badges", baseURL, url.PathEscape(username))
		if err := doJSON("POST", u, m.token, payload, nil); err != nil {
			return errMsg{err}
		}
		return badgeUpdated{}
	}
}

func (m model) removeBadge(username, badge string) tea.Cmd {
	return func() tea.Msg {
		u := fmt.Sprintf("%s/admin/users/%s/badges/%s", baseURL, url.PathEscape(username), url.PathEscape(badge))
		if err := doJSON("DELETE", u, m.token, nil, nil); err != nil {
			return errMsg{err}
		}
		return badgeUpdated{}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case postsLoaded:
		m.posts = msg.posts
		items := make([]list.Item, len(msg.posts))
		for i, p := range msg.posts {
			items[i] = PostItem{
				ID:        p.ID,
				PostTitle: p.Title,
				Desc:      fmt.Sprintf("by %s | ↑%d ↓%d | %d comments", p.Author, p.Upvotes, p.Downvotes, p.CommentCount),
			}
		}
		delegate := list.NewDefaultDelegate()
		l := list.New(items, delegate, m.width-4, m.height-4)
		l.Title = "Posts"
		m.list = &l
		m.err = nil
		return m, nil

	case postDetailsLoaded:
		m.selectedPost = &msg.post
		m.comments = msg.comments
		m.err = nil
		return m, nil

	case postCreated:
		m.state = statePostList
		m.err = nil
		m.postForm = newForm("New Post", []string{"Title", "Content"}, []bool{false, false})
		return m, m.loadPosts()

	case commentCreated:
		m.state = statePostView
		m.err = nil
		if m.selectedPost != nil {
			return m, m.loadPostDetails(m.selectedPost.ID)
		}
		return m, nil

	case voteDone:
		if m.selectedPost != nil && m.selectedPost.ID == msg.postID {
			return m, m.loadPostDetails(m.selectedPost.ID)
		}
		return m, nil

	case authSuccess:
		m.token = msg.token
		m.username = msg.username
		m.state = stateMainMenu
		m.err = nil
		m.statusOK = fmt.Sprintf("Welcome, %s!", msg.username)
		m.viewProfileIsSelf = true
		m.refreshMainMenu()
		return m, m.loadProfile(msg.username)

	case registerSuccess:
		m.err = nil
		return m, m.login(msg.username, msg.password)

	case profileLoaded:
		p := msg.profile
		m.viewedProfile = &p
		if m.viewProfileIsSelf {
			m.myProfile = &p
			m.refreshMainMenu()
		}
		if m.state == stateEditBio {
			m.state = stateProfile
		}
		m.err = nil
		return m, nil

	case usersLoaded:
		items := make([]list.Item, len(msg.profiles))
		for i, p := range msg.profiles {
			items[i] = UserItem{
				ID:       p.ID,
				Username: p.Username,
				Role:     p.Role,
				Desc:     fmt.Sprintf("joined %s | %d posts, %d comments | %d badges", p.CreatedAt, p.PostCount, p.CommentCount, len(p.Badges)),
			}
		}
		delegate := list.NewDefaultDelegate()
		l := list.New(items, delegate, m.width-4, m.height-4)
		l.Title = "Users (Admin Panel)"
		m.adminList = &l
		m.err = nil
		return m, nil

	case roleUpdated:
		m.state = stateAdminPanel
		m.statusOK = "Role updated"
		return m, m.loadAllUsers()

	case badgeUpdated:
		m.state = stateAdminPanel
		m.statusOK = "Badges updated"
		return m, m.loadAllUsers()

	case tickMsg:
		if m.state != stateSplash {
			return m, nil
		}
		m.splashFrame++
		if m.splashFrame >= 40 {
			m.state = stateAuthMenu
			return m, nil
		}
		return m, tickCmd()

	case errMsg:
		m.err = msg.error
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.list != nil {
			m.list.SetSize(msg.Width-4, msg.Height-4)
		}
		if m.adminList != nil {
			m.adminList.SetSize(msg.Width-4, msg.Height-4)
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if msg.String() == "q" && m.state != stateSplash && !m.isTypingState() {
			return m, tea.Quit
		}

		switch m.state {
		case stateSplash:
			m.state = stateAuthMenu
			return m, nil
		case stateAuthMenu:
			return m.updateAuthMenu(msg)
		case stateLogin:
			return m.updateLogin(msg)
		case stateRegister:
			return m.updateRegister(msg)
		case stateMainMenu:
			return m.updateMainMenu(msg)
		case statePostList:
			return m.updatePostList(msg)
		case statePostView:
			return m.updatePostView(msg)
		case stateNewPost:
			return m.updateNewPost(msg)
		case stateNewComment:
			return m.updateNewComment(msg)
		case stateSettings:
			return m.updateSettings(msg)
		case stateProfile:
			return m.updateProfile(msg)
		case stateEditBio:
			return m.updateEditBio(msg)
		case stateAdminPanel:
			return m.updateAdminPanel(msg)
		case stateAdminAction:
			return m.updateAdminAction(msg)
		case stateBadgeInput:
			return m.updateBadgeInput(msg)
		}
	}

	return m, nil
}

func (m model) isTypingState() bool {
	switch m.state {
	case stateLogin, stateRegister, stateNewPost, stateNewComment, stateEditBio, stateBadgeInput:
		return true
	}
	return false
}

// auth panel
func (m model) updateAuthMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.authMenu.up()
	case "down", "j":
		m.authMenu.down()
	case "enter":
		switch m.authMenu.cursor {
		case 0:
			m.state = stateLogin
			m.err = nil
		case 1:
			m.state = stateRegister
			m.err = nil
		case 2:
			return m, tea.Quit
		}
	}
	return m, nil
}

// login handling
func (m model) updateLogin(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = stateAuthMenu
		m.err = nil
		return m, nil
	case "tab", "down":
		m.loginForm.next()
		return m, nil
	case "shift+tab", "up":
		m.loginForm.prev()
		return m, nil
	case "enter":
		if m.loginForm.focus < len(m.loginForm.fields)-1 {
			m.loginForm.next()
			return m, nil
		}
		username := m.loginForm.value(0)
		password := m.loginForm.value(1)
		if username == "" || password == "" {
			m.err = fmt.Errorf("username and password are required")
			return m, nil
		}
		return m, m.login(username, password)
	default:
		cmd := m.loginForm.update(msg)
		return m, cmd
	}
}

// register handling
func (m model) updateRegister(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = stateAuthMenu
		m.err = nil
		return m, nil
	case "tab", "down":
		m.regForm.next()
		return m, nil
	case "shift+tab", "up":
		m.regForm.prev()
		return m, nil
	case "enter":
		if m.regForm.focus < len(m.regForm.fields)-1 {
			m.regForm.next()
			return m, nil
		}
		username := m.regForm.value(0)
		password := m.regForm.value(1)
		if username == "" || password == "" {
			m.err = fmt.Errorf("username and password are required")
			return m, nil
		}
		return m, m.register(username, password)
	default:
		cmd := m.regForm.update(msg)
		return m, cmd
	}
}

// main menu panel
func (m model) updateMainMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.mainMenu.up()
	case "down", "j":
		m.mainMenu.down()
	case "enter":
		opt := m.mainMenu.options[m.mainMenu.cursor]
		return m.executeMainMenuOption(opt)
	}
	return m, nil
}

func (m model) executeMainMenuOption(opt string) (tea.Model, tea.Cmd) {
	switch opt {
	case "Browse posts":
		m.state = statePostList
		return m, m.loadPosts()
	case "New post":
		m.state = stateNewPost
		m.postForm = newForm("New Post", []string{"Title", "Content"}, []bool{false, false})
		return m, nil
	case "My Profile":
		m.state = stateProfile
		m.viewProfileIsSelf = true
		return m, m.loadProfile(m.username)
	case "Settings":
		m.state = stateSettings
		return m, nil
	case "Admin Panel":
		m.state = stateAdminPanel
		return m, m.loadAllUsers()
	case "Log out":
		m.token = ""
		m.username = ""
		m.myProfile = nil
		m.viewedProfile = nil
		m.state = stateAuthMenu
		m.err = nil
		m.statusOK = ""
		m.mainMenu.cursor = 0
		m.refreshMainMenu()
		return m, nil
	case "Quit":
		return m, tea.Quit
	default:
		return m, nil
	}
}

// post list handling
func (m model) updatePostList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.list != nil {
			if item, ok := m.list.SelectedItem().(PostItem); ok {
				m.state = statePostView
				return m, m.loadPostDetails(item.ID)
			}
		}
		return m, nil
	case "esc", "backspace":
		m.state = stateMainMenu
		return m, nil
	}
	if m.list != nil {
		var cmd tea.Cmd
		*m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
	return m, nil
}

// post view handling
func (m model) updatePostView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		m.state = statePostList
		return m, m.loadPosts()
	case "c":
		m.state = stateNewComment
		ti := textinput.New()
		ti.Placeholder = "Comment text"
		ti.CharLimit = 500
		ti.Width = 50
		ti.Focus()
		m.commentTI = ti
		return m, textinput.Blink
	case "u":
		if m.selectedPost != nil {
			return m, m.votePost(m.selectedPost.ID, 1)
		}
	case "d":
		if m.selectedPost != nil {
			return m, m.votePost(m.selectedPost.ID, -1)
		}
	case "v":
		if m.selectedPost != nil {
			m.state = stateProfile
			m.viewProfileIsSelf = m.selectedPost.Author == m.username
			return m, m.loadProfile(m.selectedPost.Author)
		}
	}
	return m, nil
}

// new post handling
func (m model) updateNewPost(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = stateMainMenu
		return m, nil
	case "tab", "down":
		m.postForm.next()
		return m, nil
	case "shift+tab", "up":
		m.postForm.prev()
		return m, nil
	case "enter":
		if m.postForm.focus < len(m.postForm.fields)-1 {
			m.postForm.next()
			return m, nil
		}
		title := m.postForm.value(0)
		content := m.postForm.value(1)
		if title == "" || content == "" {
			m.err = fmt.Errorf("title and content cannot be empty")
			return m, nil
		}
		return m, m.createPost(title, content)
	default:
		cmd := m.postForm.update(msg)
		return m, cmd
	}
}

// new comment handling
func (m model) updateNewComment(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = statePostView
		return m, nil
	case "enter":
		content := m.commentTI.Value()
		if content == "" {
			m.err = fmt.Errorf("comment cannot be empty")
			return m, nil
		}
		if m.selectedPost == nil {
			m.err = fmt.Errorf("no post selected")
			return m, nil
		}
		return m, m.createComment(m.selectedPost.ID, content)
	default:
		var cmd tea.Cmd
		m.commentTI, cmd = m.commentTI.Update(msg)
		return m, cmd
	}
}

// settings panel
func (m model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		m.state = stateMainMenu
		return m, nil
	case "up", "k":
		m.settingsMenu.up()
	case "down", "j":
		m.settingsMenu.down()
	case "enter":
		if m.settingsMenu.cursor < len(themes) {
			m.themeIdx = m.settingsMenu.cursor
			m.statusOK = fmt.Sprintf("Theme set to %s", themes[m.themeIdx].Name)
		} else {
			m.state = stateMainMenu
		}
		return m, nil
	}
	return m, nil
}

// profile panel
func (m model) updateProfile(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		m.state = stateMainMenu
		return m, nil
	case "e":
		if m.viewProfileIsSelf && m.viewedProfile != nil {
			m.state = stateEditBio
			m.bioForm = newForm("Edit Bio", []string{"Bio"}, []bool{false})
			m.bioForm.fields[0].SetValue(m.viewedProfile.Bio)
			return m, textinput.Blink
		}
	}
	return m, nil
}

// edit panel
func (m model) updateEditBio(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = stateProfile
		return m, nil
	case "enter":
		bio := m.bioForm.value(0)
		return m, m.updateBio(bio)
	default:
		cmd := m.bioForm.update(msg)
		return m, cmd
	}
}

// admin panel
func (m model) updateAdminPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		m.state = stateMainMenu
		return m, nil
	case "enter":
		if m.adminList != nil {
			if item, ok := m.adminList.SelectedItem().(UserItem); ok {
				m.selectedAdminUsername = item.Username
				m.selectedAdminRole = item.Role
				m.adminActionMenu = buildAdminActionMenu(item.Role)
				m.state = stateAdminAction
			}
		}
		return m, nil
	}
	if m.adminList != nil {
		var cmd tea.Cmd
		*m.adminList, cmd = m.adminList.Update(msg)
		return m, cmd
	}
	return m, nil
}

func buildAdminActionMenu(role string) menu {
	toggle := "Promote to Admin"
	if role == "admin" {
		toggle = "Demote to User"
	}
	return menu{
		title:   "User Actions",
		options: []string{"View Profile", toggle, "Add Badge", "Remove Badge", "Back"},
	}
}

// admin handler
func (m model) updateAdminAction(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.adminActionMenu.up()
	case "down", "j":
		m.adminActionMenu.down()
	case "esc", "backspace":
		m.state = stateAdminPanel
		return m, nil
	case "enter":
		switch m.adminActionMenu.options[m.adminActionMenu.cursor] {
		case "View Profile":
			m.state = stateProfile
			m.viewProfileIsSelf = m.selectedAdminUsername == m.username
			return m, m.loadProfile(m.selectedAdminUsername)
		case "Promote to Admin":
			return m, m.setUserRole(m.selectedAdminUsername, "admin")
		case "Demote to User":
			return m, m.setUserRole(m.selectedAdminUsername, "user")
		case "Add Badge":
			m.state = stateBadgeInput
			m.badgeAction = "add"
			m.badgeForm = newForm("Add Badge", []string{"Badge name"}, []bool{false})
			return m, textinput.Blink
		case "Remove Badge":
			m.state = stateBadgeInput
			m.badgeAction = "remove"
			m.badgeForm = newForm("Remove Badge", []string{"Badge name"}, []bool{false})
			return m, textinput.Blink
		case "Back":
			m.state = stateAdminPanel
		}
		return m, nil
	}
	return m, nil
}

// badge handler
func (m model) updateBadgeInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = stateAdminAction
		return m, nil
	case "enter":
		badge := strings.TrimSpace(m.badgeForm.value(0))
		if badge == "" {
			m.err = fmt.Errorf("badge name cannot be empty")
			return m, nil
		}
		if m.badgeAction == "add" {
			return m, m.addBadge(m.selectedAdminUsername, badge)
		}
		return m, m.removeBadge(m.selectedAdminUsername, badge)
	default:
		cmd := m.badgeForm.update(msg)
		return m, cmd
	}
}

type PostItem struct {
	ID        int
	PostTitle string
	Desc      string
}

func (i PostItem) Title() string       { return i.PostTitle }
func (i PostItem) Description() string { return i.Desc }
func (i PostItem) FilterValue() string { return i.PostTitle }

type UserItem struct {
	ID       int
	Username string
	Role     string
	Desc     string
}

func (i UserItem) Title() string {
	if i.Role == "admin" {
		return i.Username + " [ADMIN]"
	}
	return i.Username
}
func (i UserItem) Description() string { return i.Desc }
func (i UserItem) FilterValue() string { return i.Username }


func splashPaletteForTheme(th Theme) []lipgloss.Color {
	return []lipgloss.Color{th.Primary, th.Accent, th.Secondary, th.Muted}
}

func (m model) viewSplash() string {
	th := themes[m.themeIdx]
	palette := splashPaletteForTheme(th)

	lines := strings.Split(strings.TrimRight(splashArt, "\n"), "\n")
	var out strings.Builder
	for i, line := range lines {
		color := palette[(i+m.splashFrame)%len(palette)]
		style := lipgloss.NewStyle().Foreground(color).Bold(true)
		out.WriteString(style.Render(line))
		out.WriteString("\n")
	}
	subStyle := lipgloss.NewStyle().Foreground(palette[m.splashFrame%len(palette)]).Bold(true)
	out.WriteString(subStyle.Render(splashSubtitle))
	out.WriteString("\n\n(press any key to continue)")
	return out.String()
}

func (m model) renderSidebar() string {
	th := themes[m.themeIdx]
	title := lipgloss.NewStyle().
		Foreground(th.Primary).
		Bold(true).
		Render("NullSec Forum")

	opts := m.mainMenu.options
	var lines []string
	for i, opt := range opts {
		style := lipgloss.NewStyle().Foreground(th.Muted)
		if i == m.mainMenu.cursor {
			style = style.Foreground(th.Accent).Bold(true)
			lines = append(lines, "▶ "+style.Render(opt))
		} else {
			lines = append(lines, "  "+style.Render(opt))
		}
	}
	menuContent := strings.Join(lines, "\n")

	content := title + "\n\n" + menuContent

	sidebar := lipgloss.NewStyle().
		Width(m.sidebarWidth).
		Padding(1, 0).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.Secondary).
		Render(content)

	return sidebar
}

// renderMain renders the right panel content based on current state.
func (m model) renderMain() string {
	th := themes[m.themeIdx]
	var body string

	switch m.state {
	case stateSplash, stateAuthMenu, stateLogin, stateRegister:
		return ""
	case stateMainMenu:
		body = "Welcome to NullSec Forum!\nSelect an option from the sidebar."
	case statePostList:
		body = m.viewPostList()
	case statePostView:
		body = m.viewPostView(th)
	case stateNewPost:
		body = m.viewNewPost()
	case stateNewComment:
		body = m.viewNewComment()
	case stateSettings:
		body = m.viewSettings()
	case stateProfile:
		body = m.viewProfile(th)
	case stateEditBio:
		body = m.viewEditBio()
	case stateAdminPanel:
		body = m.viewAdminPanel()
	case stateAdminAction:
		body = m.adminActionMenu.viewThemed(th)
	case stateBadgeInput:
		body = m.viewBadgeInput()
	default:
		body = "Unknown state"
	}

	if m.err != nil {
		body += fmt.Sprintf("\n\nError: %v", m.err)
	}
	if m.statusOK != "" {
		body += fmt.Sprintf("\n\n%s", m.statusOK)
	}

	return lipgloss.NewStyle().
		Width(m.width - m.sidebarWidth - 4).
		Padding(1, 2).
		Render(body)
}

func (m model) viewPostList() string {
	if m.list == nil {
		return "Loading..."
	}
	return m.list.View() + "\n(↑/↓ to move, Enter to open, Esc to go back)"
}

func (m model) viewPostView(th Theme) string {
	if m.selectedPost == nil {
		return "Loading..."
	}
	p := m.selectedPost
	headerStyle := lipgloss.NewStyle().Foreground(th.Primary).Bold(true)
	s := headerStyle.Render(fmt.Sprintf("=== %s ===", p.Title)) + "\n"
	s += fmt.Sprintf("Author: %s\nDate: %s\nUpvotes: %d  Downvotes: %d\n\n%s\n\n--- Comments ---\n",
		p.Author, p.CreatedAt, p.Upvotes, p.Downvotes, p.Content)
	if len(m.comments) == 0 {
		s += "No comments yet.\n"
	} else {
		for _, c := range m.comments {
			s += fmt.Sprintf("[%s] %s (↑%d ↓%d)\n", c.Author, c.Content, c.Upvotes, c.Downvotes)
		}
	}
	s += "\n[c] new comment  [u] upvote  [d] downvote  [v] view author's profile  [Esc] back"
	return s
}

func (m model) viewNewPost() string {
	return m.postForm.view()
}

func (m model) viewNewComment() string {
	return "=== New Comment ===\n\n" + m.commentTI.View() + "\n\n(Enter to submit, Esc to cancel)"
}

func (m model) viewSettings() string {
	th := themes[m.themeIdx]
	titleStyle := lipgloss.NewStyle().Foreground(th.Primary).Bold(true)
	cursorStyle := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	s := titleStyle.Render("=== Settings: Color Palette ===") + "\n\n"
	for i, t := range themes {
		cursor := "  "
		if i == m.settingsMenu.cursor {
			cursor = "> "
		}
		marker := " "
		if i == m.themeIdx {
			marker = "*"
		}
		swatch := lipgloss.NewStyle().Foreground(t.Primary).Bold(true).Render(t.Name)
		line := fmt.Sprintf("%s[%s] %s", cursor, marker, swatch)
		if i == m.settingsMenu.cursor {
			line = cursorStyle.Render(line)
		}
		s += line + "\n"
	}
	backCursor := "  "
	if m.settingsMenu.cursor == len(themes) {
		backCursor = "> "
	}
	s += fmt.Sprintf("%sBack\n", backCursor)
	s += "\n(↑/↓ to move, Enter to apply/select, Esc to go back)"
	return s
}

func (m model) viewProfile(th Theme) string {
	if m.viewedProfile == nil {
		return "Loading..."
	}
	p := m.viewedProfile
	headerStyle := lipgloss.NewStyle().Foreground(th.Primary).Bold(true)
	s := headerStyle.Render(fmt.Sprintf("=== %s's Profile ===", p.Username)) + "\n\n"

	roleLabel := p.Role
	if p.Role == "admin" {
		roleLabel = lipgloss.NewStyle().Foreground(th.Accent).Bold(true).Render("ADMIN")
	}
	s += fmt.Sprintf("Role: %s\n", roleLabel)
	s += fmt.Sprintf("Joined: %s\n", p.CreatedAt)
	s += fmt.Sprintf("Posts: %d   Comments: %d\n\n", p.PostCount, p.CommentCount)

	if len(p.Badges) > 0 {
		badgeStyle := lipgloss.NewStyle().Foreground(th.Secondary).Bold(true)
		badgeStrs := make([]string, len(p.Badges))
		for i, b := range p.Badges {
			badgeStrs[i] = badgeStyle.Render("[" + b + "]")
		}
		s += "Badges: " + strings.Join(badgeStrs, " ") + "\n\n"
	} else {
		s += "Badges: none\n\n"
	}

	bio := p.Bio
	if bio == "" {
		bio = "(no bio set)"
	}
	s += "Bio:\n" + bio + "\n\n"

	if m.viewProfileIsSelf {
		s += "[e] edit bio  "
	}
	s += "[Esc] back"
	return s
}

func (m model) viewEditBio() string {
	return m.bioForm.view()
}

func (m model) viewAdminPanel() string {
	if m.adminList == nil {
		return "Loading..."
	}
	return m.adminList.View() + "\n(↑/↓ to move, Enter to manage user, Esc to go back)"
}

func (m model) viewBadgeInput() string {
	return m.badgeForm.view()
}

func (m model) View() string {
	switch m.state {
	case stateSplash:
		return m.viewSplash()
	case stateAuthMenu:
		return m.authMenu.viewThemed(themes[m.themeIdx])
	case stateLogin:
		return m.loginForm.view()
	case stateRegister:
		return m.regForm.view()
	}

	sidebar := m.renderSidebar()
	main := m.renderMain()

	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main)
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}