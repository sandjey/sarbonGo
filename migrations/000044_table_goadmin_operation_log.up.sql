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
-- Name: goadmin_operation_log; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_operation_log (
    id integer NOT NULL,
    user_id integer NOT NULL,
    path character varying(255) NOT NULL,
    method character varying(10) NOT NULL,
    ip character varying(15) NOT NULL,
    input text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_operation_log_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.goadmin_operation_log_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: goadmin_operation_log_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.goadmin_operation_log_id_seq OWNED BY public.goadmin_operation_log.id;


--
-- Name: goadmin_operation_log id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_operation_log ALTER COLUMN id SET DEFAULT nextval('public.goadmin_operation_log_id_seq'::regclass);


--
-- Name: goadmin_operation_log goadmin_operation_log_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_operation_log
    ADD CONSTRAINT goadmin_operation_log_pkey PRIMARY KEY (id);


--
-- Name: goadmin_operation_log_user_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goadmin_operation_log_user_id_idx ON public.goadmin_operation_log USING btree (user_id);


--
-- PostgreSQL database dump complete
--


