# Plan: Import file-browser features from ccmux-master into ccmux-main

Nguồn: `ccmux-master` (Rust, ratatui) có file-tree sidebar + preview
syntax-highlight + mouse resize mà `ccmux-main` (Go) chưa có đầy đủ.
Quyết định phạm vi (đã chốt với user 2026-08-01):

- **Không** làm lại split-pane tự vẽ (PTY/vt100 riêng) — tmux đã làm
  việc này, giữ nguyên triết lý "tmux = session store, ccmux = lens".
- **Có** làm: file-tree cho mọi loại file (không chỉ `.md`), preview
  syntax-highlight, mouse click-focus/drag-resize, cd tracking tự động.
- Người code: Claude Code, qua nhiều phiên làm việc. Ước lượng bên dưới
  tính theo **số phiên agent** (1 phiên ≈ agent code + tự test/build,
  chưa tính review của bạn), kèm quy đổi giờ công tương đương để dễ hình dung.

Điểm thuận lợi đã xác nhận khi khảo sát code: `internal/tui/notes.go`
(1675 dòng) đã có sẵn ~80% logic cần thiết — cây thư mục thu/phóng, ô
preview, tìm kiếm, chuyển project — chỉ giới hạn ở file `.md`. Tận dụng
lại thay vì viết mới. `chroma` (syntax highlight) đã có sẵn as indirect
dep qua Glamour. tmux đã tự track cwd của pane qua
`#{pane_current_path}` — không cần tự parse OSC7 như ccmux-master.

Quy ước dự án: mỗi feature phải lên **openspec change proposal**
trước, và phải có mặt ở cả TUI *và* CLI (feature surface policy trong
CLAUDE.md), kèm test cho từng phần.

Tổng ước lượng: **~10–13 phiên agent** (~28–42 giờ công quy đổi).

Repo: [github.com/hoangnh32it/ccmux](https://github.com/hoangnh32it/ccmux)
(public, khởi tạo 2026-08-01 với baseline commit trước khi bắt đầu
Phase 1). **Quy ước: sau khi hoàn thành mỗi phase bên dưới, commit +
push lên `main` ngay** — mỗi phase là 1 commit riêng, message nêu rõ
phase nào vừa xong, để lịch sử git phản ánh đúng tiến độ tài liệu này.

---

## Phase 0 — Spec & thiết kế (~1 phiên, 2–3h) ✅ DONE 2026-08-01

- [x] Viết openspec proposal (`openspec/changes/file-browser/proposal.md`,
      `design.md`, `tasks.md`, `specs/file-browser-support/spec.md`)
      theo đúng format Why / What Changes / Capabilities như
      `add-grok-agent` (openspec CLI không cài local nên validate bằng
      tay theo template, không chạy được `openspec validate`)
- [x] Quyết định: **màn hình mới** `ScreenFiles` (không generalize
      `notesModel`) — Notes giữ nguyên phạm vi markdown-only, Files là
      package/model riêng (`internal/filebrowser`, `internal/tui/files.go`),
      tái dùng pattern fold/unfold + split-pane từ notes.go
- [x] Vị trí tab: **append cuối cùng** trong `Screen` enum, sau
      `ScreenNetwork` → phím `8`. Lý do: `screenKey()` suy ra số thứ tự
      từ vị trí enum, chèn giữa sẽ đổi số phím của mọi tab phía sau —
      append là lựa chọn an toàn duy nhất (giống cách Grok agent được
      thêm cuối `All()` để giữ thứ tự ổn định)
- [x] Chốt thêm: syntax highlight dùng `chroma` trực tiếp (không qua
      Glamour, Glamour vẫn chỉ dành cho Notes/markdown); cwd tracking
      qua `tmux display-message -p '#{pane_current_path}'` thay vì OSC7
- [x] Tasks.md đã liệt kê chi tiết theo từng phase 2–8 tiếp theo, khớp
      với danh sách trong file kế hoạch này
- [x] Init git repo cho `ccmux-main`, tạo GitHub repo
      `hoangnh32it/ccmux` (public), push baseline commit

## Phase 1 — File-tree cho mọi loại file (~2 phiên, 5–7h) ✅ DONE 2026-08-01

- [x] Tạo package mới `internal/filebrowser` (`filebrowser.go`) walk
      toàn bộ project tree, không lọc theo `.md`, vẫn bỏ qua
      `.git`/`node_modules`/build dirs — `skipDir` giữ **y hệt**
      `internal/notes.skipDir` để Notes và Files thống nhất về "cái gì
      thuộc về project". Chỉ liệt kê regular file (bỏ symlink/socket/
      device — đi theo symlink dir dễ ra ngoài cây hoặc lặp vô hạn).
      Thư mục không đọc được (permission denied) bị skip chứ không làm
      hỏng cả listing
- [x] Tái sử dụng logic thu/phóng thư mục từ `notes.go`, nhưng tách ra
      thành **hàm thuần** trong `tree.go` (`VisibleRows`, `FolderDirs`,
      `DefaultFolds`, `ParentDir`, `DescendantOf`) thay vì method trên
      model — không phụ thuộc Bubble Tea nên test được trực tiếp, và
      Phase 3 chỉ còn việc nối dây
- [x] Thêm binary-file detection (`binary.go`): sniff tối đa 8000 byte
      (ngưỡng của git), coi là binary nếu có byte NUL hoặc không phải
      UTF-8 hợp lệ. Sniff **lười** — chỉ chạy trên file sắp preview,
      không chạy cho từng dòng listing (mở 614 file mỗi lần render là
      không chấp nhận được)
- [x] Unit test cho walk logic + fold/unfold + binary detection
      (table-driven): 30 test, `go test ./...` sạch (48 package)
- [x] Commit + push lên GitHub (`git push origin main`) khi Phase 1 xong

**Ngoài checklist:** `Tree.Resolve` kiểm tra containment (chặn `..`,
đường dẫn tuyệt đối, và symlink trỏ ra ngoài cây) trước khi `Read`.
Phase 6 sẽ nhận `<path>` từ dòng lệnh và — với `--host` — từ mạng, nên
đây là thuộc tính đúng-sai của primitive đọc file, không phải tính năng
thêm.

**Cần quyết định trước Phase 3:** `bin/` không nằm trong `skipDir`
(notes cũng không có), nên khi browse chính repo ccmux thì 3 binary đã
build hiện ra trong cây. Nếu muốn ẩn, thêm `"bin"` vào `skipDir` —
nhưng lúc đó Files sẽ lệch khỏi Notes, và `bin/` là source thật ở một
số project (npm `bin/cli.js`).

## Phase 2 — Preview syntax-highlight (~2 phiên, 5–7h) ✅ DONE 2026-08-01

- [x] Viết renderer dùng `alecthomas/chroma/v2` (`highlight.go`), đã
      promote indirect → direct trong `go.mod`. Style
      **`catppuccin-mocha`** — trùng đúng bảng màu của
      `styles.DefaultPalette`, nên code highlight nằm cùng thế giới màu
      với phần chrome xung quanh thay vì chửi nhau. Formatter
      `terminal256` (không phải `terminal16m`: chroma ghi escape thẳng
      vào string, không đi qua bộ downsample color-profile của lipgloss)
- [x] Map extension → lexer qua `lexers.Match`, không nhận diện được thì
      trả về **content nguyên xi** (không dùng `lexers.Fallback` — nó sẽ
      bọc cả file trong escape mà chẳng tô màu gì)
- [x] Giữ nguyên đường Glamour cho `.md`: `Preview.Render()` trả markdown
      thô để filesModel đưa cho Glamour, chroma cho mọi loại khác
- [x] Giới hạn kích thước: `HighlightLimit` 256 KiB (quá thì plain text)
      và `PreviewLimit` 1 MiB (quá thì chỉ đọc phần đầu, `Truncated=true`).
      Chọn file 2 GB tốn đúng bằng chọn file 2 KB
- [x] Commit + push lên GitHub (`git push origin main`) khi Phase 2 xong

**Kiến trúc:** `Tree.Preview(rel)` làm I/O + phân loại (binary?
truncated? lexer nào? có highlight nổi không?) và trả **text thô**;
chọn renderer là việc của caller. TUI gọi Glamour cho `.md` và
`Highlight` cho phần còn lại, `ccmux files read` in thẳng. Nếu nhét
nhánh rẽ đó vào trong package thì CLI lại phải bóc ANSI ra khỏi output
của chính nó.

**Ghi chú `go mod tidy`:** lệnh này đồng thời sửa 4 dep bị đánh dấu sai
`// indirect` từ trước (`termenv`, `apns2`, `golang.org/x/term`,
`modernc.org/sqlite` — repo dùng trực tiếp cả 4). Đồ thị module không
đổi, chỉ comment dịch chỗ. Kích thước binary **không tăng** vì Glamour
đã kéo sẵn toàn bộ lexer chroma vào rồi.

## Phase 3 — Màn hình TUI mới + wiring (~2 phiên, 5–8h) ✅ DONE 2026-08-01

- [x] Thêm `ScreenFiles` vào cuối enum `Screen` (phím `8`) — header,
      `allScreens()`, `screenLabels` tự ăn theo. Thêm binding `8`/`f8`
      trong `keys.go`
- [x] Model `filesModel` (`internal/tui/files.go`, 944 dòng — bằng ~56%
      `notes.go` vì logic cây đã nằm ở `internal/filebrowser` dạng hàm
      thuần): 2 pane tree/preview, focus switch, fold/unfold, scroll,
      project picker, walk **và** đọc preview đều async
- [x] Style: chỉ dùng token trong `internal/tui/styles/` —
      `styles_lint_test.go` pass
- [x] 5 golden test: `files`, `files_preview`, `files_binary`,
      `files_empty`, `files_narrow`. Golden preview dùng formatter
      `noop` của chroma nên snapshot không có escape — 256-màu thật sẽ
      nhét hàng trăm escape vào file, không review nổi mà chẳng chứng
      minh thêm điều gì so với test của package
- [x] Commit + push lên GitHub (`git push origin main`) khi Phase 3 xong

**Ngoài checklist — bắt buộc phải sửa:** hint `"1-7"` ("screens") được
viết tay ở **8 help bar khác nhau**; thêm tab thứ 8 làm cả 8 chỗ nói
dối cùng lúc. Thay bằng `screenKeyRange()` suy từ `screenCount`, và
thêm lint `TestNoLiteralTabKeyRange` — đúng nhóm bug mà
`TestNoLiteralTabKeyDigits` đã chặn cho digit đơn, chỉ khác hình dạng.

**Quyết định thu hẹp phạm vi:** Files **không** có search (ripgrep) và
**không** có chuyển device `H` như Notes. Spec chỉ yêu cầu `--host` ở
phía CLI (Phase 6), không yêu cầu ở TUI. Cũng không cache entry theo
project như Notes: mục đích của cây Files là thấy đĩa **ngay lúc này**,
cache cả phiên sẽ giấu đúng những file agent vừa ghi ra.

## Phase 4 — Mouse: click-focus + drag-resize (~1–2 phiên, 4–6h) ✅ DONE 2026-08-01

- [x] Kiểm tra rồi: router `tea.MouseMsg` trong `app.go` **chỉ** forward
      wheel, còn lại nuốt hết. Giờ forward cả non-wheel — nhưng **chỉ
      cho Files**. Các màn khác vẫn nuốt, để một cú click lạc không làm
      dịch selection mà người dùng đang không nhìn
- [x] Toạ độ chuột đến ở hệ terminal tuyệt đối, trong khi màn hình chỉ
      biết các dòng nó tự vẽ → `bodyMouse()` trừ đi `headerRows`.
      `TestHeaderRowsMatchesRenderedHeader` ghim hằng số này vào chiều
      cao thật của tab strip (nếu tab strip thành 2 dòng thì **mọi** cú
      click trên Files lệch 1 dòng mà không ai biết)
- [x] Drag border (`internal/tui/files_mouse.go`): press vào border
      (±1 cột slack — mục tiêu rộng 1 ô thì không ai bấm trúng) bật
      `draggingSplit`, motion tính lại `splitRatio`, release tắt. Viewport
      preview được lay lại **và** render lại nội dung ở bề rộng mới,
      không thì chữ vẫn wrap theo cột cũ
- [x] Test: `TestRatioForX` (hàm thuần, cả 2 biên clamp, 2 phía của
      biên min, và terminal rộng 0 — xảy ra thật trước `WindowSizeMsg`
      đầu tiên) + 10 test tương tác qua `Update`.
      `TestClickSelectsRow` **suy Y từ output render thật** thay vì
      hardcode offset, nên số học dòng không mục nát khi header pane đổi
- [x] Commit + push lên GitHub (`git push origin main`) khi Phase 4 xong

## Phase 5 — cd tracking tự động (~1 phiên, 3–4h) ✅ DONE 2026-08-01

- [x] `tmux.CurrentPath(ctx, session)` dùng `display-message -p
      '#{pane_current_path}'`, không parse OSC7. Tách `parseCurrentPath`
      thành hàm thuần để test được output giả mà không cần tmux server
      thật. Session không tồn tại → trả `""` **không** kèm lỗi (giống
      `PaneTitle`): vòng poll không được bắt đầu báo lỗi chỉ vì session
      nó đang theo dõi vừa thoát
- [x] Hook vào tick 2 giây sẵn có của App, nhưng **chỉ poll khi màn
      Files đang hiển thị**. Một `tmux display-message` mỗi tick thì rẻ,
      nhưng không miễn phí, và cái cây không ai nhìn thì không cần mới
- [x] `attachedLocalSession()` chọn session để theo: đúng **một** local
      session đang attach, hoặc không theo gì cả. Hai session cùng attach
      thì không có câu trả lời duy nhất, chọn bừa sẽ làm cây nhảy qua lại
      giữa 2 project ở các tick xen kẽ. Session remote bị bỏ qua — cwd của
      nó là đường dẫn trên đĩa máy khác, máy này không walk được
- [x] Test: `TestParseCurrentPath` bảng 8 dạng output giả; `TestCwdMsgRerootsTree`
      xác nhận root đổi **và** cây mới được walk thật; thêm test cho
      no-op cùng path, path rỗng, session cũ, toggle `f`, và ưu tiên
      pick tay
- [x] Commit + push lên GitHub (`git push origin main`) khi Phase 5 xong

**Quyết định UX:** tracking bật mặc định (file browser mà lờ đi chỗ bạn
đang thật sự đứng là mặc định sai), có dấu `⇢` trên dòng path để câu
"sao cây tự nhảy" luôn có câu trả lời nhìn thấy được. Phím `f` bật/tắt.
Chọn project bằng tay ở picker sẽ **tự tắt** tracking — người dùng vừa
chỉ đích danh cái cây họ muốn, tick sau giật đi là hành xử thù địch.

## Phase 6 — CLI parity (feature surface policy) (~1 phiên, 2–3h)

- [ ] Thêm subcommand kiểu `ccmux files list|read <project> [--host]`
      mirroring `ccmux notes list|read|search` đã có
- [ ] Cobra command file mới trong `cmd/ccmux/cmd/`
- [ ] Test CLI (giống test hiện có cho notes CLI)
- [ ] Commit + push lên GitHub (`git push origin main`) khi Phase 6 xong

## Phase 7 — Tests tổng hợp + fuzz (nếu áp dụng) (~1 phiên, 3–4h)

- [ ] `go test ./...` sạch trước khi coi là xong (yêu cầu bắt buộc của
      repo)
- [ ] e2e test trong `internal/e2e/` cho CUJ mới: mở Files screen, click
      chọn file, xem preview, resize
- [ ] Cross-compile check `GOOS=windows`, `GOOS=linux` nếu đụng code
      OS-specific (mouse/path handling)
- [ ] Commit + push lên GitHub (`git push origin main`) khi Phase 7 xong

## Phase 8 — Docs (~0.5–1 phiên, 1–2h)

- [ ] Cập nhật `README.md` (mục feature list + có thể thêm 1 GIF demo
      theo `docs/vhs/`)
- [ ] Thêm trang `docs/02_Architecture/0X_File_Browser.md` mô tả kiến
      trúc màn hình mới, theo mẫu `01_Notes_System.md`
- [ ] Cập nhật `CLAUDE.md` phần Components nếu thêm package mới
      (`internal/filebrowser`)
- [ ] Commit + push lên GitHub (`git push origin main`) khi Phase 8 xong

---

## Ghi chú tiến độ

Claude sẽ tự tick `[x]` vào từng dòng trên khi hoàn thành trong các
phiên code tiếp theo, để bạn biết đang ở đâu mà không cần đọc lại toàn
bộ hội thoại.
