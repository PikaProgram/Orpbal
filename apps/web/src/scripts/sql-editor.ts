import { EditorView, basicSetup } from "codemirror";
import { sql, PostgreSQL } from "@codemirror/lang-sql";

// Mounts a CodeMirror 6 SQL editor on each textarea.sql-editor, syncing the
// editor content back into the textarea so plain form posts keep working.
function mount(el: HTMLTextAreaElement) {
	const view = new EditorView({
		doc: el.value,
		extensions: [
			basicSetup,
			sql({ dialect: PostgreSQL, upperCaseKeywords: true }),
			EditorView.lineWrapping,
			EditorView.updateListener.of((update) => {
				if (update.docChanged) {
					el.value = update.state.doc.toString();
				}
			}),
		],
	});
	el.value = view.state.doc.toString();
	el.style.display = "none";
	el.parentElement?.insertBefore(view.dom, el.nextSibling);
	el.form?.addEventListener("submit", () => {
		el.value = view.state.doc.toString();
	});
}

document.querySelectorAll<HTMLTextAreaElement>("textarea.sql-editor").forEach(mount);
