package database

import (
	"time"

	"github.com/jomei/notionapi"
)

// Getters Methods

func GetTitleValue(property notionapi.Property) (string, bool) {
	title, ok := property.(*notionapi.TitleProperty)
	if !ok {
		return "", false
	}

	if len(title.Title) == 0 {
		return "", false
	}

	return title.Title[0].PlainText, true
}

func GetCreatedTimeValue(property notionapi.Property) (time.Time, bool) {
	createdTime, ok := property.(*notionapi.CreatedTimeProperty)
	if !ok {
		return time.Time{}, false
	}

	return createdTime.CreatedTime, true
}

func GetNumberValue(property notionapi.Property) (int64, bool) {
	number, ok := property.(*notionapi.NumberProperty)
	if !ok {
		return 0, false
	}

	return int64(number.Number), true
}

func GetSelectValue(property notionapi.Property) (string, bool) {
	prop, ok := property.(*notionapi.SelectProperty)
	if !ok {
		return "", false
	}

	return prop.Select.Name, true
}

// Building Methods

func BuildTitleProperty(title string) notionapi.Property {
	return notionapi.TitleProperty{
		Title: []notionapi.RichText{
			{
				Text: &notionapi.Text{
					Content: title,
					Link:    nil,
				},
			},
		},
	}
}

func BuildRichTextProperty(text string) notionapi.Property {
	return notionapi.RichTextProperty{
		RichText: []notionapi.RichText{
			{
				Text: &notionapi.Text{
					Content: text,
					Link:    nil,
				},
			},
		},
	}
}

func BuildSelectProperty(option string) notionapi.Property {
	return notionapi.SelectProperty{
		Select: notionapi.Option{
			Name: option,
		},
	}
}

func BuildNumberProperty(number float64) notionapi.Property {
	return notionapi.NumberProperty{
		Number: number,
	}
}
