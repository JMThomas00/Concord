package client

import (
	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
)

// ChannelTreeNode represents a node in the channel tree (category or channel)
type ChannelTreeNode struct {
	Channel    *models.Channel
	Children   []*ChannelTreeNode
	Parent     *ChannelTreeNode
	IsCategory bool
}

// ChannelTree maintains a hierarchical structure of channels and categories
type ChannelTree struct {
	Root     *ChannelTreeNode               // Virtual root node
	NodeMap  map[uuid.UUID]*ChannelTreeNode // Fast lookup by channel ID
	FlatList []*ChannelTreeNode             // Flattened list for rendering
}

// BuildChannelTree constructs a tree from a flat list of channels
func BuildChannelTree(channels []*models.Channel) *ChannelTree {
	tree := &ChannelTree{
		Root: &ChannelTreeNode{
			Channel:    nil,
			Children:   []*ChannelTreeNode{},
			Parent:     nil,
			IsCategory: false,
		},
		NodeMap:  make(map[uuid.UUID]*ChannelTreeNode),
		FlatList: []*ChannelTreeNode{},
	}

	if len(channels) == 0 {
		return tree
	}

	// First pass: Create nodes for all channels
	for _, channel := range channels {
		node := &ChannelTreeNode{
			Channel:    channel,
			Children:   []*ChannelTreeNode{},
			Parent:     nil,
			IsCategory: channel.Type == models.ChannelTypeCategory,
		}
		tree.NodeMap[channel.ID] = node
	}

	// Second pass: Build parent-child relationships
	for _, channel := range channels {
		node := tree.NodeMap[channel.ID]

		// Check if channel has a parent category (non-empty UUID)
		if channel.CategoryID != uuid.Nil && channel.CategoryID != (uuid.UUID{}) {
			// This channel belongs to a category
			if parentNode, exists := tree.NodeMap[channel.CategoryID]; exists {
				node.Parent = parentNode
				parentNode.Children = append(parentNode.Children, node)
			} else {
				// Orphaned channel (category doesn't exist) - attach to root
				node.Parent = tree.Root
				tree.Root.Children = append(tree.Root.Children, node)
			}
		} else {
			// Top-level channel or category - attach to root
			node.Parent = tree.Root
			tree.Root.Children = append(tree.Root.Children, node)
		}
	}

	// Build initial flat list (all categories expanded)
	tree.RebuildFlatList(make(map[uuid.UUID]bool))

	return tree
}

// RebuildFlatList reconstructs the flat list for rendering based on collapsed state
func (t *ChannelTree) RebuildFlatList(collapsedCategories map[uuid.UUID]bool) {
	t.FlatList = []*ChannelTreeNode{}

	// Depth-first traversal of the tree
	var traverse func(node *ChannelTreeNode)
	traverse = func(node *ChannelTreeNode) {
		// Skip the virtual root
		if node == t.Root {
			for _, child := range node.Children {
				traverse(child)
			}
			return
		}

		// Add current node to flat list
		t.FlatList = append(t.FlatList, node)

		// If this is a category and it's not collapsed, traverse children
		if node.IsCategory {
			if !collapsedCategories[node.Channel.ID] {
				for _, child := range node.Children {
					traverse(child)
				}
			}
		}
	}

	traverse(t.Root)
}

// AddChannel adds a new channel to the tree
func (t *ChannelTree) AddChannel(channel *models.Channel) {
	// Check if already exists
	if _, exists := t.NodeMap[channel.ID]; exists {
		return
	}

	// Create new node
	node := &ChannelTreeNode{
		Channel:    channel,
		Children:   []*ChannelTreeNode{},
		Parent:     nil,
		IsCategory: channel.Type == models.ChannelTypeCategory,
	}
	t.NodeMap[channel.ID] = node

	// Attach to parent
	if channel.CategoryID != uuid.Nil && channel.CategoryID != (uuid.UUID{}) {
		if parentNode, exists := t.NodeMap[channel.CategoryID]; exists {
			node.Parent = parentNode
			parentNode.Children = append(parentNode.Children, node)
		} else {
			// Parent category doesn't exist - attach to root
			node.Parent = t.Root
			t.Root.Children = append(t.Root.Children, node)
		}
	} else {
		// Top-level - attach to root
		node.Parent = t.Root
		t.Root.Children = append(t.Root.Children, node)
	}
}

// RemoveChannel removes a channel from the tree
func (t *ChannelTree) RemoveChannel(channelID uuid.UUID) {
	node, exists := t.NodeMap[channelID]
	if !exists {
		return
	}

	// If this is a category, recursively remove all children
	if node.IsCategory {
		for _, child := range node.Children {
			t.RemoveChannel(child.Channel.ID)
		}
	}

	// Remove from parent's children list
	if node.Parent != nil {
		for i, child := range node.Parent.Children {
			if child == node {
				node.Parent.Children = append(node.Parent.Children[:i], node.Parent.Children[i+1:]...)
				break
			}
		}
	}

	// Remove from node map
	delete(t.NodeMap, channelID)
}

// UpdateChannel updates a channel in the tree (e.g., moved to different category)
func (t *ChannelTree) UpdateChannel(channel *models.Channel) {
	node, exists := t.NodeMap[channel.ID]
	if !exists {
		// Channel doesn't exist, add it
		t.AddChannel(channel)
		return
	}

	// Update channel data
	node.Channel = channel
	node.IsCategory = channel.Type == models.ChannelTypeCategory

	// Check if parent changed
	oldParent := node.Parent
	hasNewParent := channel.CategoryID != uuid.Nil && channel.CategoryID != (uuid.UUID{})

	var oldParentID uuid.UUID
	hasOldParent := false
	if oldParent != nil && oldParent != t.Root && oldParent.Channel != nil {
		oldParentID = oldParent.Channel.ID
		hasOldParent = true
	}

	// Parent changed - re-attach
	parentChanged := (hasOldParent != hasNewParent) || (hasOldParent && hasNewParent && oldParentID != channel.CategoryID)

	if parentChanged {
		// Remove from old parent
		if oldParent != nil {
			for i, child := range oldParent.Children {
				if child == node {
					oldParent.Children = append(oldParent.Children[:i], oldParent.Children[i+1:]...)
					break
				}
			}
		}

		// Attach to new parent
		if hasNewParent {
			if newParent, exists := t.NodeMap[channel.CategoryID]; exists {
				node.Parent = newParent
				newParent.Children = append(newParent.Children, node)
			} else {
				// New parent doesn't exist - attach to root
				node.Parent = t.Root
				t.Root.Children = append(t.Root.Children, node)
			}
		} else {
			// No category - attach to root
			node.Parent = t.Root
			t.Root.Children = append(t.Root.Children, node)
		}
	}
}
