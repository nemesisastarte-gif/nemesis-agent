import ace from "ace-builds/src-noconflict/ace";

const cssText = `

.ace-nemesiscode .ace_gutter {
  background: #f8f8f8;
  color: #2e3440
}

.ace-nemesiscode {
  background-color: #FFFFFF;
  color: #2e3440;
  line-height: 1.8 !important;
}

.ace-nemesiscode .ace_cursor {
  color: #AEAFAD
}

.ace-nemesiscode .ace_marker-layer .ace_selection {
  background: #e0e0e0
}

.ace-nemesiscode.ace_multiselect .ace_selection.ace_start {
  box-shadow: 0 0 3px 0px #FFFFFF;
}

.ace-nemesiscode .ace_marker-layer .ace_step {
  background: rgb(255, 255, 0)
}

.ace-nemesiscode .ace_marker-layer .ace_bracket {
  margin: -1px 0 0 -1px;
  border: 1px solid #D1D1D1
}

.ace-nemesiscode .ace_marker-layer .ace_active-line {
  background: #f4f4f4
}

.ace-nemesiscode .ace_gutter-active-line {
  background-color : #f4f4f4
}

.ace-nemesiscode .ace_marker-layer .ace_selected-word {
  border: 1px solid #e8e8e8
}

.ace-nemesiscode .ace_invisible {
  color: #D1D1D1
}

.ace-nemesiscode .ace_keyword,
.ace-nemesiscode .ace_meta,
.ace-nemesiscode .ace_storage,
.ace-nemesiscode .ace_storage.ace_type,
.ace-nemesiscode .ace_support.ace_type {
  color: #8959A8
}

.ace-nemesiscode .ace_keyword.ace_operator {
  color: #3E999F
}

.ace-nemesiscode .ace_constant.ace_character,
.ace-nemesiscode .ace_constant.ace_language,
.ace-nemesiscode .ace_constant.ace_numeric,
.ace-nemesiscode .ace_keyword.ace_other.ace_unit,
.ace-nemesiscode .ace_support.ace_constant,
.ace-nemesiscode .ace_variable.ace_parameter {
  color: #F5871F
}

.ace-nemesiscode .ace_constant.ace_other {
  color: #666969
}

.ace-nemesiscode .ace_invalid {
  color: #FFFFFF;
  background-color: #C82829
}

.ace-nemesiscode .ace_invalid.ace_deprecated {
  color: #FFFFFF;
  background-color: #8959A8
}

.ace-nemesiscode .ace_fold {
  background-color: #4271AE;
  border-color: #2e3440
}

.ace-nemesiscode .ace_entity.ace_name.ace_function,
.ace-nemesiscode .ace_support.ace_function,
.ace-nemesiscode .ace_variable {
  color: #C99E00
}

.ace-nemesiscode .ace_support.ace_class,
.ace-nemesiscode .ace_support.ace_type {
  color: #C99E00
}

.ace-nemesiscode .ace_string {
  color: #5e81ac;
}

.ace-nemesiscode .ace_markup {
  color: #8fbcbb !important;
}

.ace-nemesiscode .ace_heading {
  color: #5e81ac;
  font-weight: bold;
}

.ace-nemesiscode .ace_comment {
  color: #8E908C;
}

.dark .ace-nemesiscode {
  background-color: #0d1117;
  color: #c9d1d9;
}

.dark .ace-nemesiscode .ace_gutter {
  background: #161b22;
  color: #8b949e;
}

.dark .ace-nemesiscode .ace_cursor {
  color: #c9d1d9;
}

.dark .ace-nemesiscode .ace_marker-layer .ace_selection {
  background: #264f78;
}

.dark .ace-nemesiscode.ace_multiselect .ace_selection.ace_start {
  box-shadow: 0 0 3px 0 #0d1117;
}

.dark .ace-nemesiscode .ace_marker-layer .ace_step {
  background: #4b3f16;
}

.dark .ace-nemesiscode .ace_marker-layer .ace_bracket {
  border-color: #6e7681;
}

.dark .ace-nemesiscode .ace_marker-layer .ace_active-line,
.dark .ace-nemesiscode .ace_gutter-active-line {
  background: #161b22;
}

.dark .ace-nemesiscode .ace_marker-layer .ace_selected-word {
  border-color: #6e7681;
}

.dark .ace-nemesiscode .ace_invisible {
  color: #484f58;
}

.dark .ace-nemesiscode .ace_keyword,
.dark .ace-nemesiscode .ace_meta,
.dark .ace-nemesiscode .ace_storage,
.dark .ace-nemesiscode .ace_storage.ace_type,
.dark .ace-nemesiscode .ace_support.ace_type {
  color: #ff7b72;
}

.dark .ace-nemesiscode .ace_keyword.ace_operator {
  color: #79c0ff;
}

.dark .ace-nemesiscode .ace_constant.ace_character,
.dark .ace-nemesiscode .ace_constant.ace_language,
.dark .ace-nemesiscode .ace_constant.ace_numeric,
.dark .ace-nemesiscode .ace_keyword.ace_other.ace_unit,
.dark .ace-nemesiscode .ace_support.ace_constant,
.dark .ace-nemesiscode .ace_variable.ace_parameter {
  color: #79c0ff;
}

.dark .ace-nemesiscode .ace_constant.ace_other {
  color: #a5d6ff;
}

.dark .ace-nemesiscode .ace_invalid {
  color: #ffdcd7;
  background-color: #da3633;
}

.dark .ace-nemesiscode .ace_invalid.ace_deprecated {
  color: #ffdcd7;
  background-color: #8957e5;
}

.dark .ace-nemesiscode .ace_fold {
  background-color: #58a6ff;
  border-color: #c9d1d9;
}

.dark .ace-nemesiscode .ace_entity.ace_name.ace_function,
.dark .ace-nemesiscode .ace_support.ace_function,
.dark .ace-nemesiscode .ace_variable,
.dark .ace-nemesiscode .ace_support.ace_class,
.dark .ace-nemesiscode .ace_support.ace_type {
  color: #d2a8ff;
}

.dark .ace-nemesiscode .ace_string,
.dark .ace-nemesiscode .ace_heading {
  color: #a5d6ff;
}

.dark .ace-nemesiscode .ace_markup {
  color: #7ee787 !important;
}

.dark .ace-nemesiscode .ace_comment {
  color: #8b949e;
}
`;



ace.define(
    "ace/theme/nemesiscode",
    ["require", "exports", "module", "ace/lib/dom"],
    function (require: any, exports: any) {
      exports.isDark = true;
      exports.cssClass = "ace-nemesiscode";
      exports.cssText = cssText;
  
      const dom = require("ace/lib/dom");
      dom.importCssString(cssText, exports.cssClass);
    }
  );
