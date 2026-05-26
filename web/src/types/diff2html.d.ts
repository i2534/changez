declare module "diff2html/bundles/js/diff2html-ui.min.js" {
  export class Diff2HtmlUI {
    constructor(
      targetElement: HTMLElement,
      diff: string,
      config?: {
        drawFileList?: boolean;
        matching?: "lines" | "none" | "words";
        outputFormat?: "line-by-line" | "side-by-side";
        highlight?: boolean;
        fileContentToggle?: boolean;
      }
    );
    draw(): void;
  }
}
