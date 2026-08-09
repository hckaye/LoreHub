import styles from "./code-detail.module.css";

type SafeReadmeProps = { content: string; label: string };

export function SafeReadme({ content, label }: SafeReadmeProps) {
  const lines = content.split(/\r?\n/);
  const blocks: Array<{ kind: "heading" | "paragraph" | "list" | "code"; text: string }> = [];
  let code = false;
  let codeLines: string[] = [];
  let paragraph: string[] = [];
  let list: string[] = [];
  const flushParagraph = () => {
    if (paragraph.length > 0) {
      blocks.push({ kind: "paragraph", text: paragraph.join(" ") });
      paragraph = [];
    }
  };
  const flushList = () => {
    if (list.length > 0) {
      blocks.push({ kind: "list", text: list.join("\n") });
      list = [];
    }
  };
  const flushCode = () => {
    if (codeLines.length > 0) {
      blocks.push({ kind: "code", text: codeLines.join("\n") });
      codeLines = [];
    }
  };
  for (const line of lines) {
    if (line.trimStart().startsWith("```")) {
      flushParagraph();
      flushList();
      if (code) {
        flushCode();
      }
      code = !code;
      continue;
    }
    if (code) {
      codeLines.push(line);
      continue;
    }
    const heading = /^(#{1,3})\s+(.+)$/.exec(line);
    if (heading) {
      flushParagraph();
      flushList();
      blocks.push({ kind: "heading", text: `${heading[1]}\u0000${heading[2]}` });
      continue;
    }
    const item = /^\s*[-*+]\s+(.+)$/.exec(line);
    if (item) {
      flushParagraph();
      list.push(item[1]);
      continue;
    }
    if (!line.trim()) {
      flushParagraph();
      flushList();
      continue;
    }
    flushList();
    paragraph.push(line.trim());
  }
  flushParagraph();
  flushList();
  if (code) {
    flushCode();
  }
  return (
    <article aria-label={label} className={styles.readme}>
      {blocks.map((block, index) => {
        if (block.kind === "heading") {
          const [level, text] = block.text.split("\u0000");
          if (level === "#") return <h1 key={index}>{text}</h1>;
          if (level === "##") return <h2 key={index}>{text}</h2>;
          return <h3 key={index}>{text}</h3>;
        }
        if (block.kind === "list") {
          return (
            <ul key={index}>
              {block.text.split("\n").map((item, itemIndex) => (
                <li key={`${itemIndex}-${item}`}>{item}</li>
              ))}
            </ul>
          );
        }
        if (block.kind === "code") {
          return <pre key={index}>{block.text}</pre>;
        }
        return <p key={index}>{block.text}</p>;
      })}
    </article>
  );
}
