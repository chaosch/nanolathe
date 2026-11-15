package vfs

// HighestPriority 返回当前已挂载 provider 中的最高优先级。
// 空 FS 返回 0。
func (f *FS) HighestPriority() int {
	highest := 0
	for _, m := range f.mounts {
		if m.priority > highest {
			highest = m.priority
		}
	}
	return highest
}

// MountDirectoryTop 把一个目录挂到现有 VFS 的最高优先级之上。
// 它自动计算一个比当前所有 mount 都高的 priority，
// 因此之后 ReadFile/Open/Stat 都会优先命中这个目录。
func (f *FS) MountDirectoryTop(root string) error {
	priority := f.HighestPriority() + 1
	return f.MountDirectory(root, priority)
}
