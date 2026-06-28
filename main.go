package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// --- 1. МОДЕЛИ ДАННЫХ ---

type User struct {
	gorm.Model
	Username string `gorm:"uniqueIndex"`
	Email    string `gorm:"uniqueIndex"`
	Password string // Хэшированный пароль (bcrypt)
	FullName string
	Avatar   string // Ссылка на аватар в /static/uploads/
	About    string // Текст "О себе"
}

type Post struct {
	gorm.Model
	AuthorID    uint
	Author      User      `gorm:"foreignKey:AuthorID"`
	WallOwnerID uint
	WallOwner   User      `gorm:"foreignKey:WallOwnerID"`
	Content     string
	Comments    []Comment `gorm:"foreignKey:PostID"` // Связь один-ко-многим
}

type Comment struct {
	gorm.Model
	PostID   uint `gorm:"index"`
	AuthorID uint
	Author   User `gorm:"foreignKey:AuthorID"`
	Content  string
}

type Follow struct {
	FollowerID uint `gorm:"primaryKey;autoIncrement:false"`
	FolloweeID uint `gorm:"primaryKey;autoIncrement:false"`
	Accepted   bool `gorm:"default:true"`
}

type Config struct {
	Port          string `json:"port"`
	DBPath        string `json:"db_path"`
	SessionSecret string `json:"session_secret"`
	UploadDir     string `json:"upload_dir"`
	AllowSignups  bool   `json:"allow_signups"`
}

var cfg Config
var db *gorm.DB
var store *sessions.CookieStore
var templates = make(map[string]*template.Template)

// --- 2. ИНИЦИАЛИЗАЦИЯ И СБОРКА ШАБЛОНОВ ---

func initTemplates() {
	layout := "templates/layout.html"
	post := "templates/post.html"
	files := []string{"profile.html", "login.html", "register.html", "edit.html", "feed.html"}

	for _, file := range files {
		if file == "profile.html" || file == "feed.html" {
			templates[file] = template.Must(template.ParseFiles(layout, "templates/"+file, post))
		} else {
			templates[file] = template.Must(template.ParseFiles(layout, "templates/"+file))
		}
	}
}

func loadConfig() {
	cfg = Config{
		Port:          ":8080",
		DBPath:        "network.db",
		SessionSecret: "outline-default-secret-key-fallback",
		UploadDir:     "static/uploads",
		AllowSignups:  true,
	}

	file, err := os.Open("config.json")
	if err != nil {
		log.Println("Файл config.json не найден, используются настройки по умолчанию")
		return
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		log.Println("Ошибка чтения config.json:", err)
	}
}

func initDB() {
	var err error
	db, err = gorm.Open(sqlite.Open(cfg.DBPath), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}
	db.AutoMigrate(&User{}, &Post{}, &Comment{}, &Follow{})

	os.MkdirAll(cfg.UploadDir, os.ModePerm)
}

// --- 3. АВТОРИЗАЦИЯ И СЕССИИ (Middleware) ---

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if path == "/login" || path == "/register" || strings.HasPrefix(path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}

		session, _ := store.Get(r, "session")
		userID, ok := session.Values["user_id"]
		if !ok || userID == nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func getCurrentUser(r *http.Request) User {
	session, _ := store.Get(r, "session")
	userID := session.Values["user_id"].(uint)
	var u User
	db.First(&u, userID)
	return u
}

// --- 4. КОНТРОЛЛЕРЫ АВТОРИЗАЦИИ ---

func showLogin(w http.ResponseWriter, r *http.Request) {
	templates["login.html"].ExecuteTemplate(w, "layout.html", map[string]interface{}{
		"Title":        "Outline",
		"AllowSignups": cfg.AllowSignups,
		"NoLeftMenu":   false,  
		"WideLayout":   false,
	})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	var user User
	if err := db.Where("username = ?", username).First(&user).Error; err != nil {
		templates["login.html"].ExecuteTemplate(w, "layout.html", map[string]interface{}{
			"Error":        "Неверное имя пользователя или пароль",
			"AllowSignups": cfg.AllowSignups,
			"NoLeftMenu":   true,
			"WideLayout":   false,
		})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		templates["login.html"].ExecuteTemplate(w, "layout.html", map[string]interface{}{
			"Error":        "Неверное имя пользователя или пароль",
			"AllowSignups": cfg.AllowSignups,
			"NoLeftMenu":   true,
			"WideLayout":   false,
		})
		return
	}

	session, _ := store.Get(r, "session")
	session.Values["user_id"] = user.ID
	session.Save(r, w)

	http.Redirect(w, r, "/"+user.Username, http.StatusFound)
}

func showRegister(w http.ResponseWriter, r *http.Request) {
	templates["register.html"].ExecuteTemplate(w, "layout.html", nil)
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	if !cfg.AllowSignups {
		templates["register.html"].ExecuteTemplate(w, "layout.html", map[string]interface{}{
			"Error": "Регистрация временно закрыта",
		})
		return
	}

	username := strings.ToLower(r.FormValue("username"))
	fullname := r.FormValue("fullname")
	email := r.FormValue("email")
	password := r.FormValue("password")

	var existing User
	if db.Where("username = ? OR email = ?", username, email).First(&existing).RowsAffected > 0 {
		templates["login.html"].ExecuteTemplate(w, "layout.html", map[string]interface{}{
			"Error":        "Пользователь с таким именем или E-mail уже существует",
			"AllowSignups": cfg.AllowSignups,
			"NoLeftMenu":   true,
			"WideLayout":   false,
		})
		return
	}

	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

	newUser := User{
		Username: username,
		FullName: fullname,
		Email:    email,
		Password: string(hashedPassword),
	}
	db.Create(&newUser)

	session, _ := store.Get(r, "session")
	session.Values["user_id"] = newUser.ID
	session.Save(r, w)

	http.Redirect(w, r, "/"+newUser.Username, http.StatusFound)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	session.Values["user_id"] = nil
	session.Save(r, w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// --- 5. КОНТРОЛЛЕРЫ НАСТРОЕК И ЗАГРУЗОК ---

func showEdit(w http.ResponseWriter, r *http.Request) {
	currentUser := getCurrentUser(r)
	templates["edit.html"].ExecuteTemplate(w, "layout.html", map[string]interface{}{
		"Title":       "Редактирование профиля",
		"CurrentUser": currentUser,
		"NoLeftMenu":  false,
		"WideLayout":  true,
	})
}

func handleEdit(w http.ResponseWriter, r *http.Request) {
	currentUser := getCurrentUser(r)
	fullname := r.FormValue("fullname")
	about := r.FormValue("about")

	currentUser.FullName = fullname
	currentUser.About = about
	db.Save(&currentUser)

	http.Redirect(w, r, "/edit", http.StatusFound)
}

func handleUploadAvatar(w http.ResponseWriter, r *http.Request) {
	currentUser := getCurrentUser(r)

	r.ParseMultipartForm(10 << 20)
	file, handler, err := r.FormFile("avatar")
	if err != nil {
		http.Redirect(w, r, "/edit", http.StatusFound)
		return
	}
	defer file.Close()

	randomBytes := make([]byte, 8)
	rand.Read(randomBytes)
	fileName := hex.EncodeToString(randomBytes) + filepath.Ext(handler.Filename)
	uploadPath := filepath.Join(cfg.UploadDir, fileName)

	dst, err := os.Create(uploadPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()
	io.Copy(dst, file)

	currentUser.Avatar = "/static/uploads/" + fileName
	db.Save(&currentUser)

	http.Redirect(w, r, "/edit", http.StatusFound)
}

// --- 6. КЛАССИЧЕСКИЕ РОУТЫ ---

func handleProfile(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	currentUser := getCurrentUser(r)

	var profileUser User
	if err := db.Where("username = ?", username).First(&profileUser).Error; err != nil {
		http.Error(w, "Пользователь не найден", http.StatusNotFound)
		return
	}

	var posts []Post
	db.Preload("Author").
		Preload("WallOwner").
		Preload("Comments", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at asc")
		}).
		Preload("Comments.Author").
		Where("wall_owner_id = ?", profileUser.ID).
		Order("created_at desc").
		Find(&posts)

	var followCount int64
	db.Model(&Follow{}).Where("follower_id = ? AND followee_id = ?", currentUser.ID, profileUser.ID).Count(&followCount)
	isFollowing := followCount > 0

	data := map[string]interface{}{
		"Title":       profileUser.FullName + " [outline]",
		"Profile":     profileUser,
		"Posts":       posts,
		"CurrentUser": currentUser,
		"IsFollowing": isFollowing,
		"NoLeftMenu":  false,
		"WideLayout":  true,
	}

	templates["profile.html"].ExecuteTemplate(w, "layout.html", data)
}

func handleFeed(w http.ResponseWriter, r *http.Request) {
	currentUser := getCurrentUser(r)

	var followings []Follow
	db.Where("follower_id = ? AND accepted = ?", currentUser.ID, true).Find(&followings)

	authorIDs := []uint{currentUser.ID}
	for _, f := range followings {
		authorIDs = append(authorIDs, f.FolloweeID)
	}

	var posts []Post
	db.Preload("Author").
		Preload("WallOwner").
		Preload("Comments", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at asc")
		}).
		Preload("Comments.Author").
		Where("author_id IN (?)", authorIDs).
		Order("created_at desc").
		Limit(50).
		Find(&posts)

	data := map[string]interface{}{
		"Title":       "Outline - Новости",
		"Posts":       posts,
		"CurrentUser": currentUser,
		"NoLeftMenu":  false,
		"WideLayout":  true,
	}

	templates["feed.html"].ExecuteTemplate(w, "layout.html", data)
}

func handleCreatePost(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	wallOwnerUsername := r.FormValue("wall_owner")
	content := r.FormValue("content")

	currentUser := getCurrentUser(r)

	var wallOwner User
	db.Where("username = ?", wallOwnerUsername).First(&wallOwner)

	db.Create(&Post{
		AuthorID:    currentUser.ID,
		WallOwnerID: wallOwner.ID,
		Content:     content,
	})

	http.Redirect(w, r, r.Header.Get("Referer"), http.StatusFound)
}

func handleCreateComment(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	postID := r.FormValue("post_id")
	content := r.FormValue("content")

	currentUser := getCurrentUser(r)

	db.Create(&Comment{
		PostID:   parseUint(postID),
		AuthorID: currentUser.ID,
		Content:  content,
	})

	http.Redirect(w, r, r.Header.Get("Referer"), http.StatusFound)
}

func handleFollow(w http.ResponseWriter, r *http.Request) {
	targetUsername := chi.URLParam(r, "username")
	currentUser := getCurrentUser(r)

	var targetUser User
	db.Where("username = ?", targetUsername).First(&targetUser)

	if currentUser.ID != targetUser.ID {
		db.Create(&Follow{
			FollowerID: currentUser.ID,
			FolloweeID: targetUser.ID,
			Accepted:   true,
		})
	}
	http.Redirect(w, r, "/"+targetUsername, http.StatusFound)
}

func handleUnfollow(w http.ResponseWriter, r *http.Request) {
	targetUsername := chi.URLParam(r, "username")
	currentUser := getCurrentUser(r)

	var targetUser User
	db.Where("username = ?", targetUsername).First(&targetUser)

	db.Where("follower_id = ? AND followee_id = ?", currentUser.ID, targetUser.ID).Delete(&Follow{})
	http.Redirect(w, r, "/"+targetUsername, http.StatusFound)
}

func parseUint(s string) uint {
	i, _ := strconv.ParseUint(s, 10, 32)
	return uint(i)
}

func main() {
	loadConfig()
	initDB()
	initTemplates()

	store = sessions.NewCookieStore([]byte(cfg.SessionSecret))

	r := chi.NewRouter()

	r.Use(authMiddleware)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	r.Get("/login", showLogin)
	r.Post("/login", handleLogin)
	r.Get("/register", showRegister)
	r.Post("/register", handleRegister)
	r.Get("/logout", handleLogout)
	r.Post("/account/login", handleLogin)
	r.Post("/account/register", handleRegister)

	r.Get("/edit", showEdit)
	r.Post("/edit", handleEdit)
	r.Post("/edit/avatar", handleUploadAvatar)

	r.Get("/feed", handleFeed)
	r.Post("/post", handleCreatePost)
	r.Post("/comment", handleCreateComment)
	r.Post("/{username}/follow", handleFollow)
	r.Post("/{username}/unfollow", handleUnfollow)
	r.Get("/{username}", handleProfile)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		if userID, ok := session.Values["user_id"]; ok && userID != nil {
			var u User
			db.First(&u, userID)
			http.Redirect(w, r, "/"+u.Username, http.StatusFound)
		} else {
			http.Redirect(w, r, "/login", http.StatusFound)
		}
	})

	log.Println("Outline запущен на http://localhost" + cfg.Port)
	log.Fatal(http.ListenAndServe(cfg.Port, r))
}
