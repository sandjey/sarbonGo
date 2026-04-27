--
-- PostgreSQL database dump
--


-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: cargo_types; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cargo_types (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    code character varying(128) NOT NULL,
    name_ru character varying(255) NOT NULL,
    name_uz character varying(255) NOT NULL,
    name_en character varying(255) NOT NULL,
    name_tr character varying(255) NOT NULL,
    name_zh character varying(255) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: cargo_types cargo_types_code_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_types
    ADD CONSTRAINT cargo_types_code_key UNIQUE (code);


--
-- Name: cargo_types cargo_types_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_types
    ADD CONSTRAINT cargo_types_pkey PRIMARY KEY (id);


--
-- PostgreSQL database dump complete
--


