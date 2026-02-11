package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"noodle/internal/config"
	"noodle/internal/database"
	"noodle/internal/feed"
	"noodle/internal/ui"
)

type model struct {
	state        *ui.AppState
	mainModel    *ui.MainModel
	articleModel *ui.ArticleModel
	inputModel   *ui.InputModel
	currentModel tea.Model
	inputState   string // "url", "title", or ""
}

func initialModel() *model {
	// Load config
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Initialize database
	if err := database.InitDB(); err != nil {
		fmt.Printf("Error initializing database: %v\n", err)
		os.Exit(1)
	}

	// Sync feeds from config to database
	if err := syncFeedsFromConfig(cfg); err != nil {
		fmt.Printf("Error syncing feeds: %v\n", err)
	}

	state := &ui.AppState{
		Config:      cfg,
		View:        ui.MainView,
		SelectedFeedIndex: 0,
		SelectedArticleIndex: 0,
	}

	mainModel := ui.NewMainModel(state)
	return &model{
		state:        state,
		mainModel:    mainModel,
		currentModel: mainModel,
	}
}

func syncFeedsFromConfig(cfg *config.Config) error {
	for _, feedConfig := range cfg.Feeds {
		// Skip disabled feeds
		if !feedConfig.IsEnabled() {
			continue
		}
		
		// Check if feed exists in database
		dbFeed, err := database.GetFeedByURL(feedConfig.URL)
		if err != nil {
			return err
		}

		// If not in database, add it
		if dbFeed == nil {
			// Try to fetch feed to get title
			parsedFeed, err := feed.FetchAndParseFeed(feedConfig.URL)
			if err != nil {
				// If fetch fails, still add with URL as title
				title := feedConfig.Title
				if title == "" {
					title = feedConfig.URL
				}
				dbFeed = &database.Feed{
					URL:   feedConfig.URL,
					Title: title,
				}
				if err := database.SaveFeed(dbFeed); err != nil {
					return err
				}
			} else {
				// Convert and save feed
				dbFeed = feed.ConvertFeedToDBFeed(parsedFeed, feedConfig.URL, feedConfig.Title)
				if err := database.SaveFeed(dbFeed); err != nil {
					return err
				}

				// Convert and save articles
				articles := feed.ConvertItemsToArticles(parsedFeed.Items)
				if err := database.SaveArticles(dbFeed.ID, articles); err != nil {
					return err
				}
			}
		} else {
			// Update title if custom title is set
			if feedConfig.Title != "" && dbFeed.Title != feedConfig.Title {
				dbFeed.Title = feedConfig.Title
				if err := database.SaveFeed(dbFeed); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (m *model) Init() tea.Cmd {
	return m.currentModel.Init()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Store window size in state
		if m.state != nil {
			m.state.Width = msg.Width
			m.state.Height = msg.Height
		}
		if m.inputModel != nil {
			updatedModel, cmd := m.inputModel.Update(msg)
			if inputModel, ok := updatedModel.(*ui.InputModel); ok {
				m.inputModel = inputModel
			}
			return m, cmd
		}
		var cmd tea.Cmd
		m.currentModel, cmd = m.currentModel.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		// If we're in input mode, handle input first
		if m.inputModel != nil {
			var cmd tea.Cmd
			updatedModel, cmd := m.inputModel.Update(msg)
			if inputModel, ok := updatedModel.(*ui.InputModel); ok {
				m.inputModel = inputModel
			}
			return m, cmd
		}

		// Handle feed management keys at top level
		if m.state.View == ui.MainView {
			switch msg.String() {
			case "a":
				// Add feed - show input dialog for URL
				m.inputState = "url"
				m.inputModel = ui.NewInputModel(m.state, "Enter feed URL:", "url")
				m.currentModel = m.inputModel
				return m, m.inputModel.Init()
			case "e":
				// Edit feed - show input dialog for URL
				if len(m.state.Feeds) > 0 && m.state.SelectedFeedIndex < len(m.state.Feeds) {
					m.inputState = "edit_url"
					feed := m.state.Feeds[m.state.SelectedFeedIndex]
					m.inputModel = ui.NewInputModel(m.state, fmt.Sprintf("Edit URL (current: %s):", feed.URL), "edit_url")
					m.currentModel = m.inputModel
					return m, m.inputModel.Init()
				}
			}
		}

		// Handle view switching
		var cmd tea.Cmd
		updatedModel, cmd := m.currentModel.Update(msg)
		
		// Check if view changed - if a new model was returned, use it
		if articleModel, ok := updatedModel.(*ui.ArticleModel); ok {
			m.articleModel = articleModel
			m.currentModel = articleModel
			m.state.View = ui.ArticleView
			// Call Init() on the new model to ensure timer starts
			initCmd := articleModel.Init()
			return m, tea.Batch(cmd, initCmd)
		} else if mainModel, ok := updatedModel.(*ui.MainModel); ok {
			m.mainModel = mainModel
			m.currentModel = mainModel
			m.state.View = ui.MainView
		} else {
			m.currentModel = updatedModel
		}

		return m, cmd

	case ui.InputSubmitMsg:
		return m, m.handleInputSubmit(msg)

	case ui.InputCancelMsg:
		// Cancel input, return to main view
		m.inputModel = nil
		m.inputState = ""
		if m.mainModel != nil {
			m.currentModel = m.mainModel
		}
		return m, nil

	case addFeedMsg:
		return m, m.handleAddFeed(msg)

	case editFeedMsg:
		return m, m.handleEditFeed(msg)

	case ui.FeedAddedMsg, ui.FeedUpdatedMsg:
		// Reload feeds in main model
		if m.mainModel != nil {
			var cmd tea.Cmd
			updatedModel, cmd := m.mainModel.Update(msg)
			if mainModel, ok := updatedModel.(*ui.MainModel); ok {
				m.mainModel = mainModel
			}
			return m, cmd
		}
		return m, nil

	case errorMsg:
		if m.state != nil {
			m.state.Error = msg.err.Error()
		}
		return m, nil

	default:
		var cmd tea.Cmd
		m.currentModel, cmd = m.currentModel.Update(msg)
		return m, cmd
	}
}

func (m *model) View() string {
	return m.currentModel.View()
}

type addFeedMsg struct {
	url   string
	title string
}

type editFeedMsg struct {
	index int
	url   string
	title string
}

func (m *model) handleInputSubmit(msg ui.InputSubmitMsg) tea.Cmd {
	switch m.inputState {
	case "url":
		// First input was URL, now ask for title
		m.state.Message = "" // Store URL temporarily
		m.inputState = "title"
		m.inputModel = ui.NewInputModel(m.state, "Enter custom title (optional, press Enter to skip):", "title")
		m.currentModel = m.inputModel
		// Store URL in a temporary way - we'll use the first input value
		// For now, let's store it in the state message temporarily
		m.state.Message = msg.Value // Temporarily store URL
		return m.inputModel.Init()

	case "title":
		// We have both URL and title now
		url := m.state.Message // Retrieve stored URL
		title := msg.Value
		m.inputModel = nil
		m.inputState = ""
		m.currentModel = m.mainModel
		m.state.Message = ""
		return m.handleAddFeed(addFeedMsg{url: url, title: title})

	case "edit_url":
		// First input was URL, now ask for title
		m.state.Message = msg.Value // Store new URL
		m.inputState = "edit_title"
		feed := m.state.Feeds[m.state.SelectedFeedIndex]
		m.inputModel = ui.NewInputModel(m.state, fmt.Sprintf("Edit title (current: %s):", feed.Title), "edit_title")
		m.currentModel = m.inputModel
		return m.inputModel.Init()

	case "edit_title":
		// We have both URL and title now
		url := m.state.Message // Retrieve stored URL
		title := msg.Value
		m.inputModel = nil
		m.inputState = ""
		m.currentModel = m.mainModel
		m.state.Message = ""
		return m.handleEditFeed(editFeedMsg{index: m.state.SelectedFeedIndex, url: url, title: title})
	}

	return nil
}

func (m *model) handleAddFeed(msg addFeedMsg) tea.Cmd {
	return func() tea.Msg {
		// Add to config
		newFeed := config.Feed{URL: msg.url, Title: msg.title}
		if err := config.AddFeed(m.state.Config, newFeed); err != nil {
			return errorMsg{err: err}
		}

		// Fetch and save to database
		parsedFeed, err := feed.FetchAndParseFeed(msg.url)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to fetch feed: %w", err)}
		}

		dbFeed := feed.ConvertFeedToDBFeed(parsedFeed, msg.url, msg.title)
		if err := database.SaveFeed(dbFeed); err != nil {
			return errorMsg{err: err}
		}

		articles := feed.ConvertItemsToArticles(parsedFeed.Items)
		if err := database.SaveArticles(dbFeed.ID, articles); err != nil {
			return errorMsg{err: err}
		}

		return ui.FeedAddedMsg{}
	}
}

func (m *model) handleEditFeed(msg editFeedMsg) tea.Cmd {
	return func() tea.Msg {
		if msg.index >= len(m.state.Feeds) {
			return nil
		}

		oldFeed := m.state.Feeds[msg.index]
		oldURL := oldFeed.URL

		// Update in config
		for i, f := range m.state.Config.Feeds {
			if f.URL == oldURL {
				m.state.Config.Feeds[i].URL = msg.url
				m.state.Config.Feeds[i].Title = msg.title
				if err := config.SaveConfig(m.state.Config); err != nil {
					return errorMsg{err: err}
				}
				break
			}
		}

		// If URL changed, delete old feed and add new one
		if msg.url != oldURL {
			if err := database.DeleteFeedByURL(oldURL); err != nil {
				return errorMsg{err: err}
			}
		}

		// Fetch and save updated feed
		parsedFeed, err := feed.FetchAndParseFeed(msg.url)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to fetch feed: %w", err)}
		}

		dbFeed := feed.ConvertFeedToDBFeed(parsedFeed, msg.url, msg.title)
		if msg.url == oldURL {
			dbFeed.ID = oldFeed.ID
		}
		if err := database.SaveFeed(dbFeed); err != nil {
			return errorMsg{err: err}
		}

		articles := feed.ConvertItemsToArticles(parsedFeed.Items)
		if err := database.SaveArticles(dbFeed.ID, articles); err != nil {
			return errorMsg{err: err}
		}

		return ui.FeedUpdatedMsg{}
	}
}

type errorMsg struct {
	err error
}

func (e errorMsg) Error() string {
	return e.err.Error()
}


func main() {
	// Handle cleanup
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		database.CloseDB()
		os.Exit(0)
	}()

	// Start background refresh worker
	go startRefreshWorker()

	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v\n", err)
		database.CloseDB()
		os.Exit(1)
	}

	database.CloseDB()
}

func startRefreshWorker() {
	cfg, err := config.LoadConfig()
	if err != nil {
		return
	}

	ticker := time.NewTicker(time.Duration(cfg.RefreshTime) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		feeds, err := database.GetAllFeeds()
		if err != nil {
			continue
		}

		for _, dbFeed := range feeds {
			// Find feed in config to check if it's enabled
			var feedConfig *config.Feed
			for i := range cfg.Feeds {
				if cfg.Feeds[i].URL == dbFeed.URL {
					feedConfig = &cfg.Feeds[i]
					break
				}
			}

			// Skip disabled feeds
			if feedConfig != nil && !feedConfig.IsEnabled() {
				continue
			}

			// Check if it's time to refresh
			shouldRefresh := true
			if dbFeed.LastFetched != nil {
				elapsed := time.Since(*dbFeed.LastFetched)
				if elapsed < time.Duration(cfg.RefreshTime)*time.Second {
					shouldRefresh = false
				}
			}

			if shouldRefresh {
				parsedFeed, err := feed.FetchAndParseFeed(dbFeed.URL)
				if err != nil {
					continue
				}

				// Find feed in config to get custom title
				var customTitle string
				if feedConfig != nil {
					customTitle = feedConfig.Title
				}

				// Update feed
				updatedFeed := feed.ConvertFeedToDBFeed(parsedFeed, dbFeed.URL, customTitle)
				updatedFeed.ID = dbFeed.ID
				if err := database.SaveFeed(updatedFeed); err != nil {
					continue
				}

				// Update articles
				articles := feed.ConvertItemsToArticles(parsedFeed.Items)
				if err := database.SaveArticles(updatedFeed.ID, articles); err != nil {
					continue
				}
			}
		}
	}
}

