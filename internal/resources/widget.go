package resources

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WidgetRows 是小组成件固定渲染的行数；RemoteViews 不能动态 addView，只能预留槽位。
const WidgetRows = 5

// GenerateWidgetLayout 生成 res/layout/berba_widget.xml：固定 5 个行槽位。
func GenerateWidgetLayout(resDir string) error {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(`<LinearLayout xmlns:android="http://schemas.android.com/apk/res/android"` + "\n")
	b.WriteString(`    android:id="@+id/berba_widget_root"` + "\n")
	b.WriteString(`    android:layout_width="match_parent"` + "\n")
	b.WriteString(`    android:layout_height="match_parent"` + "\n")
	b.WriteString(`    android:orientation="vertical"` + "\n")
	b.WriteString(`    android:background="@drawable/berba_widget_bg"` + "\n")
	b.WriteString(`    android:padding="12dp">` + "\n")
	b.WriteString(`    <TextView` + "\n")
	b.WriteString(`        android:id="@+id/berba_widget_title"` + "\n")
	b.WriteString(`        android:layout_width="match_parent"` + "\n")
	b.WriteString(`        android:layout_height="wrap_content"` + "\n")
	b.WriteString(`        android:maxLines="1"` + "\n")
	b.WriteString(`        android:ellipsize="end"` + "\n")
	b.WriteString(`        android:textSize="15sp"` + "\n")
	b.WriteString(`        android:textStyle="bold"` + "\n")
	b.WriteString(`        android:textColor="#DE000000"/>` + "\n")
	b.WriteString(`    <TextView` + "\n")
	b.WriteString(`        android:id="@+id/berba_widget_updated"` + "\n")
	b.WriteString(`        android:layout_width="match_parent"` + "\n")
	b.WriteString(`        android:layout_height="wrap_content"` + "\n")
	b.WriteString(`        android:maxLines="1"` + "\n")
	b.WriteString(`        android:ellipsize="end"` + "\n")
	b.WriteString(`        android:textSize="10sp"` + "\n")
	b.WriteString(`        android:textColor="#66000000"/>` + "\n")
	for i := 0; i < WidgetRows; i++ {
		prefix := fmt.Sprintf("berba_widget_row%d", i)
		b.WriteString(`    <LinearLayout` + "\n")
		b.WriteString(`        android:id="@+id/` + prefix + `"` + "\n")
		b.WriteString(`        android:layout_width="match_parent"` + "\n")
		b.WriteString(`        android:layout_height="0dp"` + "\n")
		b.WriteString(`        android:layout_weight="1"` + "\n")
		b.WriteString(`        android:layout_marginTop="4dp"` + "\n")
		b.WriteString(`        android:orientation="vertical"` + "\n")
		b.WriteString(`        android:background="@drawable/berba_widget_card"` + "\n")
		b.WriteString(`        android:paddingLeft="8dp"` + "\n")
		b.WriteString(`        android:paddingRight="8dp"` + "\n")
		b.WriteString(`        android:paddingTop="4dp"` + "\n")
		b.WriteString(`        android:paddingBottom="4dp">` + "\n")
		b.WriteString(`        <TextView` + "\n")
		b.WriteString(`            android:id="@+id/` + prefix + `_title"` + "\n")
		b.WriteString(`            android:layout_width="match_parent"` + "\n")
		b.WriteString(`            android:layout_height="wrap_content"` + "\n")
		b.WriteString(`            android:maxLines="1"` + "\n")
		b.WriteString(`            android:ellipsize="end"` + "\n")
		b.WriteString(`            android:textSize="11sp"` + "\n")
		b.WriteString(`            android:textColor="#99000000"/>` + "\n")
		b.WriteString(`        <TextView` + "\n")
		b.WriteString(`            android:id="@+id/` + prefix + `_value"` + "\n")
		b.WriteString(`            android:layout_width="match_parent"` + "\n")
		b.WriteString(`            android:layout_height="wrap_content"` + "\n")
		b.WriteString(`            android:maxLines="1"` + "\n")
		b.WriteString(`            android:ellipsize="end"` + "\n")
		b.WriteString(`            android:textSize="16sp"` + "\n")
		b.WriteString(`            android:textStyle="bold"` + "\n")
		b.WriteString(`            android:textColor="#DE000000"/>` + "\n")
		b.WriteString(`        <TextView` + "\n")
		b.WriteString(`            android:id="@+id/` + prefix + `_subtitle"` + "\n")
		b.WriteString(`            android:layout_width="match_parent"` + "\n")
		b.WriteString(`            android:layout_height="wrap_content"` + "\n")
		b.WriteString(`            android:maxLines="1"` + "\n")
		b.WriteString(`            android:ellipsize="end"` + "\n")
		b.WriteString(`            android:textSize="10sp"` + "\n")
		b.WriteString(`            android:textColor="#66000000"/>` + "\n")
		b.WriteString(`    </LinearLayout>` + "\n")
	}
	b.WriteString(`</LinearLayout>` + "\n")
	return writeResource(resDir, filepath.Join("layout", "berba_widget.xml"), b.String())
}

// GenerateWidgetInfo 生成 res/xml/berba_widget_info.xml：由 Lua 主动刷新，系统不设周期。
func GenerateWidgetInfo(resDir string) error {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(`<appwidget-provider xmlns:android="http://schemas.android.com/apk/res/android"` + "\n")
	b.WriteString(`    android:minWidth="250dp"` + "\n")
	b.WriteString(`    android:minHeight="110dp"` + "\n")
	b.WriteString(`    android:initialLayout="@layout/berba_widget"` + "\n")
	b.WriteString(`    android:resizeMode="horizontal|vertical"` + "\n")
	b.WriteString(`    android:widgetCategory="home_screen"` + "\n")
	b.WriteString(`    android:updatePeriodMillis="0"/>` + "\n")
	return writeResource(resDir, filepath.Join("xml", "berba_widget_info.xml"), b.String())
}

// GenerateWidgetDrawable 生成小组件背景与卡片底：圆角 shape，随卡片裁切。
func GenerateWidgetDrawable(resDir string) error {
	bg := `<?xml version="1.0" encoding="utf-8"?>` + "\n" +
		`<shape xmlns:android="http://schemas.android.com/apk/res/android" android:shape="rectangle">` + "\n" +
		`    <corners android:radius="16dp"/>` + "\n" +
		`    <solid android:color="#F2FFFFFF"/>` + "\n" +
		`</shape>` + "\n"
	if err := writeResource(resDir, filepath.Join("drawable", "berba_widget_bg.xml"), bg); err != nil {
		return err
	}
	card := `<?xml version="1.0" encoding="utf-8"?>` + "\n" +
		`<shape xmlns:android="http://schemas.android.com/apk/res/android" android:shape="rectangle">` + "\n" +
		`    <corners android:radius="10dp"/>` + "\n" +
		`    <solid android:color="#14000000"/>` + "\n" +
		`</shape>` + "\n"
	return writeResource(resDir, filepath.Join("drawable", "berba_widget_card.xml"), card)
}

func writeResource(resDir, rel, content string) error {
	path := filepath.Join(resDir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
