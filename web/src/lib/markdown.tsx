import { Fragment, type ReactNode } from "react";

// A deliberately small markdown renderer: code fences (with a language
// tag used only to label the block - real tokenizing highlighters are a
// large dependency this playground does not need), inline code, bold,
// italic, and paragraph breaks. It is not a CommonMark implementation;
// it covers what a chat model's own output actually uses in practice.
export function renderMarkdown(text: string): ReactNode {
  const blocks = text.split(/```/);
  return (
    <>
      {blocks.map((block, i) => {
        if (i % 2 === 1) {
          const newline = block.indexOf("\n");
          const lang = newline === -1 ? "" : block.slice(0, newline).trim();
          const code = newline === -1 ? block : block.slice(newline + 1);
          return (
            <pre key={i} className="code-block" data-lang={lang}>
              <code>{code.replace(/\n$/, "")}</code>
            </pre>
          );
        }
        return (
          <Fragment key={i}>
            {block.split("\n\n").map((para, j) => (
              <p key={j}>{renderInline(para)}</p>
            ))}
          </Fragment>
        );
      })}
    </>
  );
}

function renderInline(text: string): ReactNode {
  const parts = text.split(/(\*\*[^*]+\*\*|`[^`]+`|\*[^*]+\*)/g).filter(Boolean);
  return parts.map((part, i) => {
    if (part.startsWith("**") && part.endsWith("**")) {
      return <strong key={i}>{part.slice(2, -2)}</strong>;
    }
    if (part.startsWith("`") && part.endsWith("`")) {
      return <code key={i}>{part.slice(1, -1)}</code>;
    }
    if (part.startsWith("*") && part.endsWith("*")) {
      return <em key={i}>{part.slice(1, -1)}</em>;
    }
    return <Fragment key={i}>{part}</Fragment>;
  });
}
