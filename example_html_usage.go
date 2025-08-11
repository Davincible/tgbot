package tgbot

// Example usage of HTML formatting

func ExampleHTMLMessage() []Message {
	// Example 1: Using HTML formatting with raw HTML tags
	htmlMessage := Message{
		Text:           "<b>Bold text</b>, <i>italic text</i>, <u>underlined</u>, <s>strikethrough</s>",
		HTMLFormatting: true,
	}
	
	// Example 2: Using HTML with properly escaped special characters
	// When HTMLFormatting is true, you must escape special characters yourself
	htmlWithSpecialChars := Message{
		Text:           "Price &lt; $5 &amp; quantity &gt; 10",
		HTMLFormatting: true,
	}
	
	// Example 3: Using Markdown formatting (existing behavior)
	markdownMessage := Message{
		Text:           "*Bold text*, _italic text_, ~strikethrough~",
		TextFormatting: true,
	}
	
	// Example 4: HTML with link
	htmlWithLink := Message{
		Text:           `Check out <a href="https://example.com">this link</a>!`,
		HTMLFormatting: true,
	}
	
	// Example 5: HTML with code blocks
	htmlWithCode := Message{
		Text:           `Here's some <code>inline code</code> and a <pre>code block</pre>`,
		HTMLFormatting: true,
	}
	
	// Example 6: Mixed HTML formatting with escaped characters
	htmlMixed := Message{
		Text:           `<b>Note:</b> Use &lt;code&gt; tags for code &amp; &lt;pre&gt; for blocks`,
		HTMLFormatting: true,
	}
	
	// Note: The EscapeHTML helper function is available if you need to escape text programmatically:
	// escapedText := EscapeHTML("Price < $5 & quantity > 10")
	// This would produce: "Price &lt; $5 &amp; quantity &gt; 10"
	
	return []Message{
		htmlMessage,
		htmlWithSpecialChars,
		markdownMessage,
		htmlWithLink,
		htmlWithCode,
		htmlMixed,
	}
}